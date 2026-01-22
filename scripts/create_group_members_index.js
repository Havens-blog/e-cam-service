// MongoDB 索引创建脚本 - 优化用户组成员查询性能

// 连接到数据库
use('e-cam-service');

print('========================================');
print('创建用户组成员查询索引');
print('========================================\n');

// 1. 检查现有索引
print('1. 检查现有索引');
print('------------------------------------------');
const existingIndexes = db.cloud_iam_users.getIndexes();
let hasGroupIndex = false;

existingIndexes.forEach(index => {
    if (index.key.permission_groups) {
        hasGroupIndex = true;
        print(`✅ 找到 permission_groups 索引: ${index.name}`);
        print(`   索引键: ${JSON.stringify(index.key)}`);
    }
});

if (!hasGroupIndex) {
    print('⚠️  未找到 permission_groups 索引\n');
} else {
    print('');
}

// 2. 创建复合索引
print('2. 创建复合索引');
print('------------------------------------------');

try {
    // 创建 permission_groups + tenant_id 复合索引
    const result = db.cloud_iam_users.createIndex(
        {
            "permission_groups": 1,
            "tenant_id": 1
        },
        {
            name: "idx_permission_groups_tenant",
            background: true
        }
    );

    print(`✅ 索引创建成功: ${result}`);
} catch (e) {
    if (e.code === 85 || e.codeName === 'IndexOptionsConflict') {
        print('ℹ️  索引已存在，跳过创建');
    } else {
        print(`❌ 索引创建失败: ${e.message}`);
    }
}

print('');

// 3. 验证索引
print('3. 验证索引');
print('------------------------------------------');
const indexes = db.cloud_iam_users.getIndexes();
indexes.forEach(index => {
    print(`索引名称: ${index.name}`);
    print(`  键: ${JSON.stringify(index.key)}`);
    if (index.background !== undefined) {
        print(`  后台创建: ${index.background}`);
    }
    print('');
});

// 4. 测试查询性能
print('4. 测试查询性能');
print('------------------------------------------');

// 统计用户总数
const totalUsers = db.cloud_iam_users.countDocuments({});
print(`用户总数: ${totalUsers}`);

// 测试查询（假设查询用户组 ID=1）
const groupId = 1;
const tenantId = "tenant-001";

print(`\n测试查询: 用户组 ID=${groupId}, tenant_id=${tenantId}`);

// 使用 explain 查看查询计划
const explainResult = db.cloud_iam_users.find({
    "permission_groups": groupId,
    "tenant_id": tenantId
}).explain("executionStats");

print(`查询执行时间: ${explainResult.executionStats.executionTimeMillis} ms`);
print(`扫描文档数: ${explainResult.executionStats.totalDocsExamined}`);
print(`返回文档数: ${explainResult.executionStats.nReturned}`);

if (explainResult.executionStats.executionStages.inputStage) {
    const stage = explainResult.executionStats.executionStages.inputStage;
    if (stage.indexName) {
        print(`✅ 使用索引: ${stage.indexName}`);
    } else {
        print('⚠️  未使用索引（全表扫描）');
    }
}

print('');

// 5. 查询示例
print('5. 查询示例');
print('------------------------------------------');

const members = db.cloud_iam_users.find({
    "permission_groups": groupId,
    "tenant_id": tenantId
}).limit(5).toArray();

print(`找到 ${members.length} 个成员（最多显示5个）:`);
members.forEach((member, index) => {
    print(`  ${index + 1}. ${member.username} (ID: ${member.id})`);
    print(`     用户组: [${member.permission_groups.join(', ')}]`);
});

print('');
print('========================================');
print('索引创建完成');
print('========================================');
