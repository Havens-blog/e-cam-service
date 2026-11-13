# 证书差异治理清单（189 个 diff 域，2026-11-13 数据）

**数据来源**：认证后 `GET /api/v1/certs/probes?limit=5000`，3029 个探测域的最新结果。
**口径**：`diff` = 线上服务证书指纹 ∉ 台账中覆盖该域名的证书指纹集合（`probe_service.go` 六态判定）。
**用途**：给运维一份按「修复杠杆」排序的清单——先修一张证书能解决几十个域，比逐域排查高效。

---

## 一、总览

| 状态 | 数量 | 占比 |
|---|---|---|
| consistent | 1567 | 51.7% |
| unreachable | 1212 | 40.0% |
| **diff** | **189** | 6.2% |
| wildcard_skipped | 61 | 2.0% |

- 189 个 diff 域名共对应 **45 张不同的线上证书指纹** → 平均每张错证覆盖 4.2 个域名；
- **35 / 39 个主域同时存在 consistent 与 diff 域名** → 该主域的证书链整体是正常的，diff 属于「个别域例外」（多为个别入口/老机器未更新证书），不是整站证书问题。

## 二、按杠杆排序：先修这几张证书

同一「线上指纹」= 同一张证书在服务这批域名。修一张 = 解决一批。

| # | 线上指纹(前12) | 影响域名数 | 主要主域 | 建议 |
|---|---|---|---|---|
| 1 | `f0a9e220df4c` | **51** | jlcpcb.com×14、forface3d.net×10、jlc3dp.com×5 | 单点修复 ROI 最高：定位该证书所在入口（LB/服务器），换为台账证书 |
| 2 | `f1e33126b783` | **27** | jlcerp.com×8、jlcpcb.com×6、jlcops.com×5 | 同上，第二优先 |
| 3 | `a3b0c137cb1b` | 10 | szlcmb.com×10 | 整域一套环境，疑似统一未换证 |
| 4 | `ff084d783086` | 8 | zxhwork.com×6、jlcerp.com×2 | — |
| 5 | `1efc0c068bfe` | 7 | lcsccable.com×7 | — |
| 6 | `c16a179993c9` | 6 | jlcops.com×6（oray 子域组） | 与「四级域」问题叠加，见第三节 |
| 7 | `579b194715f7` | 5 | jlc-layout.com×3、jlcerp.com×2 | **OSS 区域通配证**：`*.oss-cn-shenzhen.aliyuncs.com`，见第三节 |
| 8 | `b0b9a6625093` | 5 | jlcerp.com×3、jlcops.com×2 | — |

其余 37 张指纹各覆盖 1–4 个域（长尾，适合批量导出逐条处理，不建议在本文件展开）。

## 三、两个有明确根因的子集（可直接按结论修）

### 3.1 通配符覆盖不到的四级域（4 个，根因已确证）

`test.order-static.jlc-layout.com` / `test.pub-static` / `test.temp-static` / `test.proxy.sz-jlc.com`

- 证书 `*.jlc-layout.com` 按 RFC 6125 **只覆盖单标签**，四级域不在覆盖范围（平台 `CheckSANCover` 与单测同口径）；
- 修复：为各 test 区签 `*.order-static.jlc-layout.com` 这类二级通配，或改三级命名（详见需求卡 `docs/req-oss-cert-target.md` 根因更正节）。

### 3.2 `*.oray.jlcops.com` 组（9 个）

`console/console1/console-api/console-api1/client-api/auth-ent/p2p/rc/svr .oray.jlcops.com`

- **这些域名被 `*.oray.jlcops.com` 正常覆盖**（单标签通配），所以不是覆盖问题，而是**线上证书确实不同**（指纹 `c16a179993c9` / `53b3fa7e8b72`）；
- 建议：核对该组入口（oray 接入侧）是否仍挂旧证书。

## 四、按主域分布（便于分派）

| 主域 | diff 数 | 主域 | diff 数 |
|---|---|---|---|
| jlcpcb.com | 22 | sz-jlc.com | 6 |
| jlcops.com | 22 | jlc3dp.com | 5 |
| jlcerp.com | 19 | jlccnc.com | 5 |
| jlcmes.com | 15 | jlcmc.com | 5 |
| zxhwork.com | 13 | 其余 30 个主域 | 各 1–4 |
| forface3d.net | 12 | | |
| szlcmb.com | 10 | | |
| lcsccable.com | 7 | | |

## 五、如何复核（可复跑）

1. 拉数据：`GET /api/v1/certs/probes?limit=5000`（需证书模块只读以上权限）；
2. 过滤 `status=diff`，按 `onlineFingerprint` 前 12 位分组 → 得到本文件的「杠杆表」；
3. 按指纹反查证书主体：`GET /api/v1/certs/reverse?...` 或各云 CAS 控制台按指纹搜索，确认是哪张证（本清单未做这一步，避免猜测证书主体）。

## 六、已知未覆盖

- 本清单不含 `unreachable`（1212 个，40%）——其中已确认 252 个是重复后缀畸形域名（已由 #634 修复，待下一轮采集验证），其余待判定是内网域还是真不可达；
- 81 个随机短标签主机名（疑似 AAD/CDN 自动别名）中 17 个 consistent，说明部分真实存在，未做过滤建议。
