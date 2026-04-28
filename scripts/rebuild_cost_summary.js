// ============================================================
// 成本数据修复脚本：清理重复数据后重建汇总表
// ============================================================
// 执行方式：mongosh mongodb://user:pass@host:27017/ecam --file scripts/rebuild_cost_summary.js
//
// 使用场景：清理了 ecam_cost_unified_bill 的重复数据后，
//          汇总表 ecam_cost_daily_summary 还是旧数据，需要重建
// ============================================================
(function () {
    var db = db.getSiblingDB("ecam");

    // 1. 查看当前数据量
    var unifiedCount = db.ecam_cost_unified_bill.estimatedDocumentCount();
    var summaryCount = db.ecam_cost_daily_summary.estimatedDocumentCount();
    print("当前数据量:");
    print("  ecam_cost_unified_bill: " + unifiedCount);
    print("  ecam_cost_daily_summary: " + summaryCount);

    // 2. 清空汇总表（应用启动时会自动从明细表重建）
    print("\n清空汇总表 ecam_cost_daily_summary...");
    db.ecam_cost_daily_summary.drop();
    print("  ✅ 已清空");

    print("\n下一步:");
    print("  1. 清除 Redis 缓存: redis-cli -a <password> KEYS 'finops:cost:*' | xargs redis-cli -a <password> DEL");
    print("  2. 重启应用，汇总表会自动从明细表重建");
    print("  3. 或者等 5 分钟让 Redis 缓存自动过期");
})();
