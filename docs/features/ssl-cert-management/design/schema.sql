// =====================================================================
// SSL 证书管理功能域 — MongoDB 集合与索引定义（mongosh 可直接执行）
// e-cam-service 使用 MongoDB (mongox)。字段命名 camelCase，与 er-diagram.md、
// API DTO、Go 模型一致（如 encryptedPrivateKey）。
// MongoDB 无列级 DEFAULT：默认值由 repository 写入路径保证，注释以 DEFAULT= 标注。
// 索引含唯一约束、部分唯一索引（在途互斥强制）、TTL（探测结果过期清理）。
// =====================================================================

// ---------------------------------------------------------------------
// cert_certificates 证书台账
// ---------------------------------------------------------------------
db.createCollection("cert_certificates", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["fingerprint", "hostingStatus", "createdAt"],
    properties: {
      fingerprint:         { bsonType: "string", pattern: "^[0-9a-f]{64}$" }, // SHA256 hex，聚合主键
      commonName:          { bsonType: "string" },
      sans:                { bsonType: "array", items: { bsonType: "string" } }, // SAN 域名数组
      issuer:              { bsonType: "string" },
      serialNumber:        { bsonType: "string" },
      notBefore:           { bsonType: "date" },
      notAfter:            { bsonType: "date" },
      keyAlgorithm:        { bsonType: "string", enum: ["RSA", "ECDSA"] },
      hostingStatus:       { bsonType: "string", enum: ["complete", "fingerprint_only"] },
      encryptedPrivateKey: {                  // 信封加密私钥；仅指纹登记时可缺省整个对象；永不出现在 API 响应
        bsonType: "object",
        required: ["ciphertext", "keyVersion", "algo"],
        properties: {
          ciphertext: { bsonType: "string" },              // AES-256-GCM 密文(base64)
          keyVersion: { bsonType: "int", minimum: 1 },     // 主密钥版本，双读解密路由依据
          algo:       { bsonType: "string", enum: ["AES-256-GCM"] }
        }
      },
      certPem:             { bsonType: "string" },          // 导入时上传的证书束 PEM 原文（leaf+中间链+根）；任务 2.2 补传私钥匹配校验依据（schema 补齐：原设计未承载补传复验所需证书材料），后续云证书库上传复用；永不���现在 API 响应
      expectedDomain:      { bsonType: "string" },          // 可选，仅提示性比对
      protectUntil:        { bsonType: "date" },            // 回滚保护期截止；>=now 禁删
      expiryAlertLevel:    { bsonType: "string", enum: ["none", "L30", "L14", "L7", "expired"] }, // DEFAULT="none"，到期分级告警去重状态（仅升级触发、同级不重复）
      createdAt:           { bsonType: "date" }             // DEFAULT=now()
    }
  }}
});
db.cert_certificates.createIndex({ fingerprint: 1 },   { unique: true, name: "uk_fingerprint" }); // 去重/聚合键
db.cert_certificates.createIndex({ hostingStatus: 1 }, { name: "idx_hosting_status" });           // 台账统计
db.cert_certificates.createIndex({ notAfter: 1 },      { name: "idx_not_after" });                // 到期分级扫描

// ---------------------------------------------------------------------
// cert_references 引用扫描发现
// ---------------------------------------------------------------------
db.createCollection("cert_references", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["certFingerprint", "snapshotId", "scannedAt"],
    properties: {
      certFingerprint:       { bsonType: "string", pattern: "^[0-9a-f]{64}$" }, // 关联 certificates.fingerprint（应用层引用，非 FK 约束）；无法解析时为确定性占位指纹（sha256 命名空间化 cloudCertId，任务 3.5，仍满足 pattern）
      cloud:                 { bsonType: "string", enum: ["aliyun", "tencent", "huawei", "aws", "azure"] },
      product:               { bsonType: "string", enum: ["cdn", "dcdn", "waf", "alb", "clb", "nlb", "crd"] },
      clusterId:             { bsonType: "string" },   // K8s 集群（product=crd 时必填）
      namespace:             { bsonType: "string" },   // K8s 命名空间（product=crd 时写通，任务 3.5；5.x 变更项重构 DeployTarget 依据）
      kind:                  { bsonType: "string" },   // CRD kind（product=crd 时写通，任务 3.5）
      resourceId:            { bsonType: "string" },   // 云资源 ID / CRD 实例名
      referencedCloudCertId: { bsonType: "string" },   // 云侧证书 ID
      accountKey:            { bsonType: "string" },
      servedDomains:         { bsonType: "array", items: { bsonType: "string" } }, // ALB 监听规则提取的 served hostname（external DNS 记录→ALB 资源级 expected 对齐）
      snapshotId:            { bsonType: "string" },   // 来源扫描快照
      scannedAt:             { bsonType: "date" }      // DEFAULT=now()
    }
  }}
});
db.cert_references.createIndex({ certFingerprint: 1, cloud: 1, product: 1 }, { name: "idx_fp_cloud_product" }); // 引用查询
db.cert_references.createIndex({ snapshotId: 1 }, { name: "idx_snapshot" });                             // 按快照清理

// ---------------------------------------------------------------------
// cert_scan_snapshots 扫描快照元数据
// ---------------------------------------------------------------------
db.createCollection("cert_scan_snapshots", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["startedAt", "status"],
    properties: {
      startedAt:    { bsonType: "date" },   // 扫描新鲜度派生基准：freshness = now - startedAt
      finishedAt:   { bsonType: "date" },   // DEFAULT=null，运行中无值
      status:       { bsonType: "string", enum: ["running", "done", "failed"] }, // DEFAULT="running"；running 超 thresholds.scanTimeoutHours 由 scan-timeout 任务转 failed（释放防重锁，可重新触发扫描）
      failReason:   { bsonType: "string" }, // status=failed 时的原因码；含 SCAN_TIMED_OUT（running 超时）、SCAN_DISCOVERY_FAILED（全部通道失败）、SCAN_NO_CHANNELS（空范围）、SCAN_WRITE_FAILED（落库失败）
      partialFailures: {                    // 部分失败通道清单（任务 3.5）：某云/产品失败不阻塞其他云，记入快照元数据
        bsonType: "array",
        items: { bsonType: "object", required: ["cloud", "product", "reason"], properties: {
          cloud:   { bsonType: "string" },  // 云名；K8s 通道为空串
          product: { bsonType: "string" },  // 产品；K8s 通道为 crd
          account: { bsonType: "string" },  // 云账号名 / K8s 集群名
          reason:  { bsonType: "string" }   // 失败原因（静态文案+安全参数，不含凭证）
        }}
      },
      coverageMeta: {                      // [{cloud,product,covered,total,lagging}]
        bsonType: "array",
        items: { bsonType: "object", required: ["cloud", "product", "covered", "total"], properties: {
          cloud:   { bsonType: "string" },
          product: { bsonType: "string" },
          covered: { bsonType: "int", minimum: 0 }, // 本轮发现引用的去重资源数
          total:   { bsonType: "int", minimum: -1 }, // 分母=internal/asset 资产盘点在用资源数；-1=分母不可用（盲区声明）；K8s crd 恒 -1（asset 不盘点 K8s）
          lagging: { bsonType: "bool" }    // DEFAULT=false；covered>total（asset 盘点滞后）时置位，覆盖率展示以 covered 为准（任务 3.5）
        }}
      }
    }
  }}
});
db.cert_scan_snapshots.createIndex({ startedAt: -1 }, { name: "idx_started_at_desc" }); // 最新成功快照查询

// ---------------------------------------------------------------------
// cert_change_orders 变更单
// ---------------------------------------------------------------------
// 状态枚举与 API 展示态映射：draft=草稿 pending_confirm=待确认 executing=执行中
// verifying=验证中 completed=已完成 partial_completed=部分完成
// rolled_back=已回滚 rollback_failed=回滚失败 cancelled=取消（终态：
// draft/pending_confirm 人工取消、批间暂停人工取消或 pauseTimeoutHours 超时自动中止）
db.createCollection("cert_change_orders", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["oldCertFingerprint", "newCertId", "status", "snapshotId", "creator", "createdAt"],
    properties: {
      oldCertFingerprint: { bsonType: "string", pattern: "^[0-9a-f]{64}$" }, // 指纹聚合键
      newCertId:          { bsonType: "string" },
      status:             { bsonType: "string", enum: ["draft", "pending_confirm", "executing", "verifying", "completed", "partial_completed", "rolled_back", "rollback_failed", "cancelled"] },
      batchInfo: {                                    // DEFAULT=null（未分批）
        bsonType: "object", required: ["totalBatches", "currentBatch", "batchSize"], properties: {
          totalBatches: { bsonType: "int", minimum: 1 },
          currentBatch: { bsonType: "int", minimum: 1 },
          batchSize:    { bsonType: "int", minimum: 1 },
          paused:       { bsonType: "bool" },          // DEFAULT=false，批间暂停待人工续批（分批一律人工续批）
          pausedAt:     { bsonType: "date" }           // 暂停起始，pauseTimeoutHours 超时自动取消基准
        }
      },
      snapshotId:        { bsonType: "string" },      // 绑定的扫描快照（确认时点重校验依据）
      verifyWindowUntil: { bsonType: "date" },        // 验证窗口截止；每批进入 verifying 时刷新
      protectUntil:      { bsonType: "date" },        // 回滚保护期截止
      activeMutex:       { bsonType: "string" },      // 在途互斥 token：活跃态=oldCertFingerprint，终态 $unset；部分唯一索引强制同一指纹仅一张活跃单
      verifyExpected: {                               // 验证窗口预期终态快照；批执行完成时固化（分批单每批按该批域名刷新）
        bsonType: "object", required: ["newCertFingerprint", "domains", "windowUntil"], properties: {
          newCertFingerprint: { bsonType: "string", pattern: "^[0-9a-f]{64}$" },
          domains:            { bsonType: "array", items: { bsonType: "string" } },
          excludedDomains:    { bsonType: "array", items: { bsonType: "string" } }, // 构建时剔除的豁免域名；计 skipped，不参与达标判定
          windowUntil:        { bsonType: "date" }
        }
      },
      creator:           { bsonType: "string" },
      createdAt:         { bsonType: "date" }         // DEFAULT=now()
    }
  }}
});
db.cert_change_orders.createIndex(
  { activeMutex: 1 },
  { unique: true, name: "uk_active_mutex",
    partialFilterExpression: { activeMutex: { $type: "string" } } }
); // 在途互斥强制：同 token 第二张活跃单 duplicate key → CHANGE_IN_FLIGHT
db.cert_change_orders.createIndex({ oldCertFingerprint: 1, status: 1 }, { name: "idx_old_fp_status" }); // 历史单查询（终态）

// ---------------------------------------------------------------------
// cert_change_items 变更项（逐项执行）
// ---------------------------------------------------------------------
db.createCollection("cert_change_items", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["orderId", "action", "resourceRef", "status"],
    properties: {
      orderId: { bsonType: "string" },
      batchNo:        { bsonType: "int", minimum: 1 },  // 批次归属，Confirm 时固化；执行仅取 batchNo=batchInfo.currentBatch 的项
      action:         { bsonType: "string", enum: ["upload_and_bind", "patch_crd"] },
      resourceRef:    { bsonType: "object" },  // 持久化完整 DeployTarget；分支必填由下方 anyOf 按 action 强制
      oldCloudCertId: { bsonType: "string" },  // 回滚依据
      newCloudCertId: { bsonType: "string" },
      status:         { bsonType: "string", enum: ["pending", "running", "success", "failed", "rate_limited", "rolled_back", "rollback_failed", "skipped"], description: "DEFAULT=pending；rollback_failed=回滚动作自身失败（任务 5.8，立即告警转人工）" },
      error:          { bsonType: "string" },   // 失败错误码+详情；异步子任务状态载体，非 HTTP 响应
      heartbeatAt:    { bsonType: "date" },     // 执行心跳（子任务运行期 30s 间隔更新）；executing-timeout 判据：running 且超 itemHeartbeatTimeoutMinutes → failed(EXEC_TIMEOUT)+告警
      executedAt:     { bsonType: "date" },     // DEFAULT=null；领取���行权时固化（crd-recheck 延迟基准）
      recheckedAt:    { bsonType: "date" }      // crd-recheck 单轮复检完成时点（任务 5.9）：success 且缺失的 patch_crd 项才消费；通过/失败均固化（复检次数固定 1）
    },
    // resourceRef 按 action 分支必填——异步子任务仅凭持久化数据即可重构 DeployTarget：
    // upload_and_bind(cloud_api): channel+cloud+product+accountKey+resourceId
    // patch_crd(k8s_api):        channel+clusterId+namespace+kind+resourceId
    allOf: [
      { anyOf: [
        { properties: {
            action: { enum: ["upload_and_bind"] },
            resourceRef: { bsonType: "object", required: ["channel", "cloud", "product", "accountKey", "resourceId"], properties: {
              channel:    { enum: ["cloud_api"] },
              cloud:      { bsonType: "string" },
              product:    { bsonType: "string", enum: ["cdn", "dcdn", "waf", "alb", "clb", "nlb"] },
              accountKey: { bsonType: "string" },
              resourceId: { bsonType: "string" }
            }}
        }},
        { properties: {
            action: { enum: ["patch_crd"] },
            resourceRef: { bsonType: "object", required: ["channel", "clusterId", "namespace", "kind", "resourceId"], properties: {
              channel:    { enum: ["k8s_api"] },
              clusterId:  { bsonType: "string" },
              namespace:  { bsonType: "string" },
              kind:       { bsonType: "string" },
              resourceId: { bsonType: "string" }
            }}
        }}
      ]}
    ]
  }}
});
db.cert_change_items.createIndex({ orderId: 1 },                    { name: "idx_order" });             // 变更单明细
db.cert_change_items.createIndex({ orderId: 1, status: 1 },         { name: "idx_order_status" });      // 批次进度统计
db.cert_change_items.createIndex({ orderId: 1, batchNo: 1 },        { name: "idx_order_batch" });       // 当前批执行取项
db.cert_change_items.createIndex(
  { status: 1, heartbeatAt: 1 },
  { name: "idx_status_heartbeat", partialFilterExpression: { status: "running" } }
); // executing-timeout 扫描：running 且心跳超时的项（executing 态活性保障）

// ---------------------------------------------------------------------
// cert_cloud_cert_mappings 平台证书↔云证书ID映射
// ---------------------------------------------------------------------
db.createCollection("cert_cloud_cert_mappings", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["certFingerprint", "cloud", "accountKey", "cloudCertId", "uploadedAt", "status"],
    properties: {
      certFingerprint: { bsonType: "string", pattern: "^[0-9a-f]{64}$" },
      cloud:           { bsonType: "string" },
      accountKey:      { bsonType: "string" },
      cloudCertId:     { bsonType: "string" },
      uploadedAt:      { bsonType: "date" },                          // DEFAULT=now()
      status:          { bsonType: "string", enum: ["active", "orphan"] } // DEFAULT="active"
    }
  }}
});
db.cert_cloud_cert_mappings.createIndex(
  { certFingerprint: 1, cloud: 1, accountKey: 1 },
  { unique: true, name: "uk_fp_cloud_account" } // 两段式去重
);

// ---------------------------------------------------------------------
// cert_probe_results TLS 探测结果
// ---------------------------------------------------------------------
db.createCollection("cert_probe_results", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["domain", "probeAt", "status"],
    properties: {
      domain:            { bsonType: "string" },
      probeAt:           { bsonType: "date" },                              // DEFAULT=now()
      onlineFingerprint: { bsonType: "string" },                            // unreachable 时缺省
      onlineNotAfter:    { bsonType: "date" },
      status:            { bsonType: "string", enum: ["consistent", "diff", "change_linked_diff", "unreachable", "exempt", "wildcard_skipped"] }, // wildcard_skipped=通配符 SAN 默认跳过拨测（可经 wildcardProbeOverrides 指定具体子域名替代探测）
      changeOrderId:     { bsonType: "string" },                            // status=change_linked_diff 时关联变更单（验证窗口内预期切换）
      tenantId:          { bsonType: "long" },                              // DNS 源探测：记录所属租户（SAN 探测缺省）
      linkedResource:    { bsonType: "string" }                             // DNS 源探测：链路关联资源类型（cdn/waf/external；SAN 探测缺省）
    }
  }}
});
db.cert_probe_results.createIndex({ domain: 1, probeAt: -1 }, { name: "idx_domain_probe_desc" });      // 最近探测查询
db.cert_probe_results.createIndex({ probeAt: 1 }, { expireAfterSeconds: 7776000, name: "ttl_probe_90d" }); // TTL：90 天自动清理

// ---------------------------------------------------------------------
// cert_exemptions 探测豁免清单
// ---------------------------------------------------------------------
db.createCollection("cert_exemptions", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["domain", "createdAt"],
    properties: {
      domain:    { bsonType: "string" },  // 唯一
      reason:    { bsonType: "string" },
      operator:  { bsonType: "string" },
      createdAt: { bsonType: "date" }     // DEFAULT=now()
    }
  }}
});
db.cert_exemptions.createIndex({ domain: 1 }, { unique: true, name: "uk_domain" });

// ---------------------------------------------------------------------
// cert_alert_config 全局告警配置（单文档，固定 _id="global"）
// ---------------------------------------------------------------------
db.createCollection("cert_alert_config", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["thresholds"],
    properties: {
      webhookUrls:      { bsonType: "array", items: { bsonType: "string" } }, // DEFAULT=[]
      emailGroup:       { bsonType: "array", items: { bsonType: "string" } }, // DEFAULT=[]
      channelConfirmed: { bsonType: "bool" },                                 // DEFAULT=false
      thresholds: {                            // 越界 400；界值与 PRD 量级一致
        bsonType: "object",
        required: ["scanFreshnessHours", "verifyWindowHours", "rollbackProtectDays", "verifyConfirmProbes", "verifyProbeIntervalMinutes", "pauseTimeoutHours", "recheckDelayMinutes", "itemHeartbeatTimeoutMinutes", "scanTimeoutHours", "expiryLevels"],
        properties: {
          scanFreshnessHours:         { bsonType: "int", minimum: 1, maximum: 72 },    // DEFAULT=24，范围 1~72
          verifyWindowHours:          { bsonType: "int", minimum: 2, maximum: 24 },    // DEFAULT=24，范围 2~24；上限 24h < rollbackProtectDays 下界 7d，区间结构已保证保护期恒长于验证窗口
          rollbackProtectDays:        { bsonType: "int", minimum: 7, maximum: 14 },    // DEFAULT=7，范围 7~14
          verifyConfirmProbes:        { bsonType: "int", minimum: 1, maximum: 10 },    // DEFAULT=2，窗口达标所需连续一致探测次数
          verifyProbeIntervalMinutes: { bsonType: "int", minimum: 5, maximum: 60 },    // DEFAULT=10，验证窗口内提频探测周期（分钟）；窗口关闭/达标回落天级
          pauseTimeoutHours:          { bsonType: "int", minimum: 24, maximum: 168 },  // DEFAULT=72，批间暂停超时自动取消（互斥活性保障）
          recheckDelayMinutes:         { bsonType: "int", minimum: 1, maximum: 60 },   // DEFAULT=5，K8s patch 后 CRD 引用复检延迟（分钟，单轮复检，crd-recheck 任务）
          itemHeartbeatTimeoutMinutes: { bsonType: "int", minimum: 5, maximum: 180 },  // DEFAULT=30，执行项心跳超时（分钟）；超时标 failed+告警、单据状态重算（executing-timeout 任务）
          scanTimeoutHours:            { bsonType: "int", minimum: 1, maximum: 12 },   // DEFAULT=2，扫描快照 running 超时转 failed 释放防重锁（scan-timeout 任务）
          expiryLevels:               { bsonType: "array", uniqueItems: true, minItems: 1, maxItems: 5, items: { bsonType: "int", minimum: 1, maximum: 90 } } // DEFAULT=[30,14,7] 到期分级天数
        }
      },
      verifyWindowRoute: {                     // 验证窗口告警路由；DEFAULT=null（复用常规通道+标记）
        bsonType: "object", required: ["enabled"], properties: {
          enabled:     { bsonType: "bool" },                                 // DEFAULT=false
          webhookUrls: { bsonType: "array", items: { bsonType: "string" } }, // 变更关联通道 webhook
          emailGroup:  { bsonType: "array", items: { bsonType: "string" } }  // 变更关联通道接收人
        }
      },
      wildcardProbeOverrides: {               // 通配符 SAN → 具体探测子域名（concreteSubdomainOverride）；DEFAULT={}
        bsonType: "object", additionalProperties: { bsonType: "string" }
      }
    }
  }}
});

// ---------------------------------------------------------------------
// cert_k8s_credentials K8s 集群接入凭证
// ---------------------------------------------------------------------
db.createCollection("cert_k8s_credentials", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["clusterName", "kubeconfig", "createdAt"],
    properties: {
      clusterName: { bsonType: "string" },   // 唯一
      kubeconfig: {                          // 加密存储，同私钥加密体系
        bsonType: "object", required: ["ciphertext", "keyVersion", "algo"], properties: {
          ciphertext: { bsonType: "string" },
          keyVersion: { bsonType: "int", minimum: 1 },
          algo:       { bsonType: "string", enum: ["AES-256-GCM"] }
        }
      },
      apiEndpoint: { bsonType: "string" },
      createdAt:   { bsonType: "date" }      // DEFAULT=now()
    }
  }}
});
db.cert_k8s_credentials.createIndex({ clusterName: 1 }, { unique: true, name: "uk_cluster_name" });

// ---------------------------------------------------------------------
// cert_batch_sessions 批量导入会话（进度轮询数据源）
// ---------------------------------------------------------------------
db.createCollection("cert_batch_sessions", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["status", "files", "progress", "operator", "createdAt"],
    properties: {
      status:    { bsonType: "string", enum: ["running", "completed", "partial_failed"] }, // DEFAULT="running"
      files: {                                        // 逐文件结果；部分失败不阻塞其他文件，失败文件可单独重试
        bsonType: "array",
        items: { bsonType: "object", required: ["fileName", "result"], properties: {
          fileName:    { bsonType: "string" },
          result:      { bsonType: "string", enum: ["pending", "success", "failed"] },
          certId:      { bsonType: "string" },        // result=success 时有值
          errorReason: { bsonType: "string" }         // result=failed 时错误码+详情
        }}
      },
      progress:  {                                    // {total,done,failed}；repository 写路径随文件完成原子递增
        bsonType: "object", required: ["total", "done", "failed"], properties: {
          total:  { bsonType: "int", minimum: 1 },
          done:   { bsonType: "int", minimum: 0 },
          failed: { bsonType: "int", minimum: 0 }
        }
      },
      operator:   { bsonType: "string" },
      createdAt:  { bsonType: "date" },               // DEFAULT=now()
      finishedAt: { bsonType: "date" }                // DEFAULT=null
    }
  }}
});
db.cert_batch_sessions.createIndex({ createdAt: 1 }, { expireAfterSeconds: 2592000, name: "ttl_batch_session_30d" }); // TTL：30 天自动清理

// ---------------------------------------------------------------------
// cert_discovery_import_sessions 云端发现导入会话（cert-cloud-discovery-import；
// 发现→登记两步操作的后端会话载体，进度轮询数据源；条目以三元组定位，
// 区别于批量导入的 fileName 主键语义）
// ---------------------------------------------------------------------
db.createCollection("cert_discovery_import_sessions", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["status", "items", "progress", "operator", "createdAt"],
    properties: {
      status:    { bsonType: "string", enum: ["running", "completed", "partial_failed"] }, // DEFAULT="running"
      items: {                                        // 逐条目结果；部分失败不中断会话，失败条目可重跑（幂等）
        bsonType: "array",
        items: { bsonType: "object", required: ["cloud", "accountKey", "cloudCertId", "result"], properties: {
          cloud:        { bsonType: "string" },        // 云（aliyun/tencent/aws/azure…）
          accountKey:   { bsonType: "string" },        // 云账号键
          cloudCertId:  { bsonType: "string" },        // 云侧证书 ID（AWS 为 ARN 形态）
          result:       { bsonType: "string", enum: ["pending", "success", "failed"] },
          mappedCertId: { bsonType: "string" },        // result=success 时有值（台账证书 ID）
          errorReason:  { bsonType: "string" }         // result=failed 时错误码+静态文案
        }}
      },
      progress:  {                                    // {total,succeeded,failed}；repository 写路径随条目完成原子递增
        bsonType: "object", required: ["total", "succeeded", "failed"], properties: {
          total:     { bsonType: "int", minimum: 1 },
          succeeded: { bsonType: "int", minimum: 0 },
          failed:    { bsonType: "int", minimum: 0 }
        }
      },
      operator:   { bsonType: "string" },
      createdAt:  { bsonType: "date" },               // DEFAULT=now()；TTL 基准
      finishedAt: { bsonType: "date" }                // DEFAULT=null；终态由 progress.failed 判定（failed>0 → partial_failed）
    }
  }}
});
db.cert_discovery_import_sessions.createIndex({ createdAt: 1 }, { expireAfterSeconds: 2592000, name: "ttl_discovery_import_session_30d" }); // TTL：30 天自动清理

// ---------------------------------------------------------------------
// cert_crd_registrations 自定义 CRD 扫描登记（K8sAPIChannel 扫描范围联动）
// ---------------------------------------------------------------------
db.createCollection("cert_crd_registrations", {
  validator: { $jsonSchema: {
    bsonType: "object",
    required: ["clusterId", "apiGroup", "kind", "certFieldPath", "enabled", "operator", "createdAt"],
    properties: {
      clusterId:     { bsonType: "string" },   // 关联 k8s_credentials 集群；登记限定单集群
      apiGroup:      { bsonType: "string" },   // 如 alb.alibabacloud.com；core 组资源为空串
      kind:          { bsonType: "string" },   // CRD kind
      certFieldPath: { bsonType: "string" },   // 云托管证书引用字段路径，如 spec.certificates[].certificateId
      enabled:       { bsonType: "bool" },     // DEFAULT=true；false=停用登记（该 CRD 回归盲区）
      operator:      { bsonType: "string" },
      createdAt:     { bsonType: "date" }      // DEFAULT=now()
    }
  }}
});
db.cert_crd_registrations.createIndex(
  { clusterId: 1, apiGroup: 1, kind: 1 },
  { unique: true, name: "uk_cluster_group_kind" } // 登记去重
);
