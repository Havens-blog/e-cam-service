#!/bin/bash

# 快速检查 Tenant ID 配置
# 使用方法: ./quick_check_tenant.sh

MONGO_URI="${MONGO_URI:-mongodb://admin:password@localhost:27017}"
DATABASE="${DATABASE:-e_cam_service}"

echo "=== 快速检查 Tenant ID 配置 ==="
echo ""

# 检查 MongoDB 连接
echo "1. 检查 MongoDB 连接..."
if ! mongosh "$MONGO_URI/$DATABASE" --quiet --eval "db.runCommand({ ping: 1 })" > /dev/null 2>&1; then
    echo "   ❌ MongoDB 连接失败"
    echo "   请检查 MONGO_URI: $MONGO_URI"
    exit 1
fi
echo "   ✓ MongoDB 连接成功"
echo ""

# 检查租户
echo "2. 检查租户..."
TENANT_COUNT=$(mongosh "$MONGO_URI/$DATABASE" --quiet --eval "db.tenants.countDocuments({})")
echo "   租户数量: $TENANT_COUNT"

if [ "$TENANT_COUNT" -eq 0 ]; then
    echo "   ⚠️  没有租户数据，请先创建租户"
    exit 1
fi

echo "   租户列表:"
mongosh "$MONGO_URI/$DATABASE" --quiet --eval "db.tenants.find({}, {_id: 1, name: 1}).forEach(t => print('     - ' + t._id + ' (' + t.name + ')'))"
echo ""

# 检查云账号
echo "3. 检查云账号..."
ACCOUNT_COUNT=$(mongosh "$MONGO_URI/$DATABASE" --quiet --eval "db.cloud_accounts.countDocuments({})")
echo "   云账号数量: $ACCOUNT_COUNT"

if [ "$ACCOUNT_COUNT" -gt 0 ]; then
    echo "   云账号 tenant_id:"
    mongosh "$MONGO_URI/$DATABASE" --quiet --eval "
        db.cloud_accounts.find({}, {id: 1, name: 1, tenant_id: 1}).forEach(a => {
            var valid = db.tenants.findOne({_id: a.tenant_id}) != null;
            var status = valid ? '✓' : '❌';
            print('     ' + status + ' ID:' + a.id + ' ' + a.name + ' -> ' + (a.tenant_id || '<空>'));
        })
    "
fi
echo ""

# 检查用户
echo "4. 检查用户..."
USER_COUNT=$(mongosh "$MONGO_URI/$DATABASE" --quiet --eval "db.cloud_iam_users.countDocuments({})")
echo "   用户数量: $USER_COUNT"

if [ "$USER_COUNT" -gt 0 ]; then
    echo "   用户按 tenant_id 分布:"
    mongosh "$MONGO_URI/$DATABASE" --quiet --eval "
        db.cloud_iam_users.aggregate([
            { \$group: { _id: '\$tenant_id', count: { \$sum: 1 } } },
            { \$sort: { count: -1 } }
        ]).forEach(s => {
            var tid = s._id || '<空>';
            var valid = s._id && db.tenants.findOne({_id: s._id}) != null;
            var status = valid ? '✓' : '❌';
            print('     ' + status + ' ' + tid + ': ' + s.count + ' 个用户');
        })
    "
fi
echo ""

# 检查用户组
echo "5. 检查用户组..."
GROUP_COUNT=$(mongosh "$MONGO_URI/$DATABASE" --quiet --eval "db.cloud_iam_groups.countDocuments({})")
echo "   用户组数量: $GROUP_COUNT"

if [ "$GROUP_COUNT" -gt 0 ]; then
    echo "   用户组按 tenant_id 分布:"
    mongosh "$MONGO_URI/$DATABASE" --quiet --eval "
        db.cloud_iam_groups.aggregate([
            { \$group: { _id: '\$tenant_id', count: { \$sum: 1 } } },
            { \$sort: { count: -1 } }
        ]).forEach(s => {
            var tid = s._id || '<空>';
            var valid = s._id && db.tenants.findOne({_id: s._id}) != null;
            var status = valid ? '✓' : '❌';
            print('     ' + status + ' ' + tid + ': ' + s.count + ' 个用户组');
        })
    "
fi
echo ""

echo "=== 检查完成 ==="
echo ""
echo "如果发现 ❌ 标记，说明 tenant_id 配置有问题"
echo "运行修复脚本: go run scripts/fix_tenant_id.go"
