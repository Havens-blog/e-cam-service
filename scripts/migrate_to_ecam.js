// ============================================================
// MongoDB 迁移脚本：ecmdb → ecam（换库 + 集合改名）
// ============================================================
// 执行方式：mongosh mongodb://user:pass@host:27017/admin --file scripts/migrate_to_ecam.js
// 支持断点续传：中断后重新执行，会从上次位置继续
// ============================================================
(function () {
    var SOURCE_DB = "ecmdb";
    var TARGET_DB = "ecam";
    var BATCH_SIZE = 2000;
    var LARGE_THRESHOLD = 500000;

    var sourceDB = db.getSiblingDB(SOURCE_DB);
    var targetDB = db.getSiblingDB(TARGET_DB);

    var renameMap = {
        "cloud_assets": "ecam_cloud_asset",
        "cloud_accounts": "ecam_cloud_account",
        "c_instance": "ecam_instance",
        "c_instance_relation": "ecam_instance_relation",
        "c_model": "ecam_model",
        "c_model_group": "ecam_model_group",
        "c_attribute": "ecam_attribute",
        "c_attribute_group": "ecam_attribute_group",
        "c_model_relation_type": "ecam_model_relation_type",
        "c_endpoint": "ecam_endpoint",
        "c_service_tree_node": "ecam_service_tree_node",
        "c_resource_binding": "ecam_resource_binding",
        "c_binding_rule": "ecam_binding_rule",
        "c_environment": "ecam_environment",
        "c_tag_policy": "ecam_tag_policy",
        "c_tag_rule": "ecam_tag_rule",
        "c_dict_type": "ecam_dict_type",
        "c_dict_item": "ecam_dict_item",
        "c_dns_domain": "ecam_dns_domain",
        "c_dns_record": "ecam_dns_record",
        "cloud_iam_users": "ecam_iam_user",
        "c_cloud_user_groups": "ecam_iam_user_group",
        "cloud_policy_templates": "ecam_iam_policy_template",
        "cloud_sync_tasks": "ecam_iam_sync_task",
        "tenants": "ecam_tenant",
        "cloud_audit_logs": "ecam_audit_log",
        "asset_change_history": "ecam_change_history",
        "tasks": "ecam_task",
        "vm_templates": "ecam_vm_template",
        "provision_tasks": "ecam_provision_task",
        "cost_raw_bills": "ecam_cost_raw_bill",
        "cost_unified_bills": "ecam_cost_unified_bill",
        "cost_daily_summary": "ecam_cost_daily_summary",
        "cost_budgets": "ecam_cost_budget",
        "cost_allocations": "ecam_cost_allocation",
        "cost_allocation_rules": "ecam_cost_allocation_rule",
        "cost_allocation_default_policy": "ecam_cost_allocation_default_policy",
        "cost_anomalies": "ecam_cost_anomaly",
        "cost_recommendations": "ecam_cost_recommendation",
        "cost_collect_logs": "ecam_cost_collect_log",
        "alert_rules": "ecam_alert_rule",
        "alert_events": "ecam_alert_event",
        "notification_channels": "ecam_notification_channel",
        "topo_nodes": "ecam_topo_node",
        "topo_edges": "ecam_topo_edge",
        "topo_declarations": "ecam_topo_declaration",
        "c_id_generator": "ecam_id_generator",
    };

    // 分页迁移：按 _id 排序分页读取，不依赖 cursor 长连接
    function migrateByPages(sourceColl, targetColl, sourceCount) {
        var migrated = 0;
        var lastId = null;

        while (true) {
            var query = lastId ? { _id: { $gt: lastId } } : {};
            var docs = sourceColl.find(query).sort({ _id: 1 }).limit(BATCH_SIZE).toArray();

            if (docs.length === 0) break;

            targetColl.insertMany(docs, { ordered: false });
            migrated += docs.length;
            lastId = docs[docs.length - 1]._id;

            if (migrated % 100000 < BATCH_SIZE) {
                var pct = Math.round(migrated / sourceCount * 100);
                print("    进度: " + migrated + "/" + sourceCount + " (" + pct + "%)");
            }
        }
        return migrated;
    }

    print("============================================================");
    print("  迁移: " + SOURCE_DB + " → " + TARGET_DB);
    print("  映射: " + Object.keys(renameMap).length + " 个集合");
    print("  支持断点续传（已迁移的自动跳过）");
    print("============================================================\n");

    var successCount = 0;
    var skipCount = 0;
    var errorCount = 0;
    var keys = Object.keys(renameMap);

    for (var i = 0; i < keys.length; i++) {
        var oldName = keys[i];
        var newName = renameMap[oldName];
        var sourceColl = sourceDB.getCollection(oldName);
        var sourceCount = sourceColl.estimatedDocumentCount();

        if (sourceCount === 0) {
            print("[跳过] " + oldName + " — 空");
            skipCount++;
            continue;
        }

        var targetColl = targetDB.getCollection(newName);
        var targetCount = targetColl.estimatedDocumentCount();

        // 已完成：目标数量 >= 源数量
        if (targetCount >= sourceCount) {
            print("[跳过] " + oldName + " → " + newName + " — 已完成 (" + targetCount + ")");
            skipCount++;
            continue;
        }

        // 部分完成：断点续传
        if (targetCount > 0) {
            print("[续传] " + oldName + " → " + newName + " (已有 " + targetCount + "/" + sourceCount + ")");
            // 找到目标集合最大的 _id，从这之后继续
            var lastDoc = targetColl.find({}).sort({ _id: -1 }).limit(1).toArray();
            if (lastDoc.length > 0) {
                var remaining = sourceColl.countDocuments({ _id: { $gt: lastDoc[0]._id } });
                print("    剩余: " + remaining + " 条");
                try {
                    var lastId = lastDoc[0]._id;
                    var migrated = targetCount;
                    while (true) {
                        var docs = sourceColl.find({ _id: { $gt: lastId } }).sort({ _id: 1 }).limit(BATCH_SIZE).toArray();
                        if (docs.length === 0) break;
                        targetColl.insertMany(docs, { ordered: false });
                        migrated += docs.length;
                        lastId = docs[docs.length - 1]._id;
                        if (migrated % 100000 < BATCH_SIZE) {
                            var pct = Math.round(migrated / sourceCount * 100);
                            print("    进度: " + migrated + "/" + sourceCount + " (" + pct + "%)");
                        }
                    }
                    var final = targetColl.estimatedDocumentCount();
                    print("  ✅ 续传完成: " + final + "/" + sourceCount);
                    successCount++;
                } catch (e) {
                    print("  ❌ 续传失败: " + e.message);
                    errorCount++;
                }
                continue;
            }
        }

        // 全新迁移
        try {
            if (sourceCount > LARGE_THRESHOLD) {
                print("[分页] " + oldName + " → " + newName + " (" + sourceCount + " 条)...");
                migrateByPages(sourceColl, targetColl, sourceCount);
            } else {
                print("[迁移] " + oldName + " → " + newName + " (" + sourceCount + " 条)...");
                sourceColl.aggregate([
                    { $match: {} },
                    { $out: { db: TARGET_DB, coll: newName } }
                ]);
            }
            var verified = targetColl.estimatedDocumentCount();
            if (verified >= sourceCount) {
                print("  ✅ " + verified + "/" + sourceCount);
                successCount++;
            } else {
                print("  ⚠️  " + verified + "/" + sourceCount + " (可重新执行续传)");
                errorCount++;
            }
        } catch (e) {
            print("  ❌ " + e.message);
            // $out 失败回退到分页
            if (sourceCount <= LARGE_THRESHOLD) {
                print("  ↻ 回退到分页模式...");
                try {
                    targetColl.drop();
                    migrateByPages(sourceColl, targetColl, sourceCount);
                    var v2 = targetColl.estimatedDocumentCount();
                    print("  ✅ " + v2 + "/" + sourceCount);
                    successCount++;
                } catch (e2) {
                    print("  ❌ " + e2.message);
                    errorCount++;
                }
            } else {
                errorCount++;
            }
        }
    }

    // 检查未映射集合
    print("\n[检查] 未映射的集合...");
    var allColls = sourceDB.getCollectionNames().filter(function (n) {
        return !n.startsWith("system.");
    });
    var unmapped = allColls.filter(function (n) {
        return keys.indexOf(n) === -1;
    });
    if (unmapped.length > 0) {
        unmapped.forEach(function (n) {
            print("  ⚠️  " + n + " (" + sourceDB.getCollection(n).estimatedDocumentCount() + " 条)");
        });
    } else {
        print("  全部已映射 ✅");
    }

    // 复制索引
    print("\n[索引] 复制...");
    for (var j = 0; j < keys.length; j++) {
        try {
            var indexes = sourceDB.getCollection(keys[j]).getIndexes();
            for (var k = 0; k < indexes.length; k++) {
                var idx = indexes[k];
                if (idx.name === "_id_") continue;
                var opts = {};
                if (idx.unique) opts.unique = true;
                if (idx.sparse) opts.sparse = true;
                if (idx.expireAfterSeconds !== undefined) opts.expireAfterSeconds = idx.expireAfterSeconds;
                if (idx.name) opts.name = idx.name;
                try {
                    targetDB.getCollection(renameMap[keys[j]]).createIndex(idx.key, opts);
                } catch (ei) { }
            }
        } catch (ec) { }
    }
    print("  ✅ 完成");

    print("\n============================================================");
    print("  成功: " + successCount + "  跳过: " + skipCount + "  失败: " + errorCount);
    if (errorCount > 0) {
        print("  💡 失败的集合可以直接重新执行脚本，会自动断点续传");
    }
    print("============================================================");
})();
