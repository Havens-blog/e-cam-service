#!/bin/bash

# 快速检查用户组成员

echo "=========================================="
echo "快速检查用户组成员"
echo "=========================================="
echo ""

# MongoDB 连接信息
MONGO_URI="mongodb://admin:Aa123456@localhost:27017"
DB_NAME="e-cam-service"

echo "1. 检查用户组数量"
echo "------------------------------------------"
mongosh "$MONGO_URI/$DB_NAME" --quiet --eval "db.cloud_iam_groups.countDocuments({})" | tail -1
echo ""

echo "2. 检查用户数量"
echo "------------------------------------------"
mongosh "$MONGO_URI/$DB_NAME" --quiet --eval "db.cloud_iam_users.countDocuments({})" | tail -1
echo ""

echo "3. 检查有 permission_groups 的用户数量"
echo "------------------------------------------"
mongosh "$MONGO_URI/$DB_NAME" --quiet --eval "db.cloud_iam_users.countDocuments({permission_groups: {\$exists: true, \$ne: []}})" | tail -1
echo ""

echo "4. 检查没有 permission_groups 的用户数量"
echo "------------------------------------------"
mongosh "$MONGO_URI/$DB_NAME" --quiet --eval "db.cloud_iam_users.countDocuments({\$or: [{permission_groups: {\$exists: false}}, {permission_groups: []}]})" | tail -1
echo ""

echo "5. 查看第一个用户组的成员"
echo "------------------------------------------"
mongosh "$MONGO_URI/$DB_NAME" --quiet --eval "
var group = db.cloud_iam_groups.findOne({});
if (group) {
    print('用户组: ' + group.name + ' (ID: ' + group.id + ')');
    print('user_count: ' + group.user_count);
    var members = db.cloud_iam_users.find({permission_groups: group.id}).toArray();
    print('实际成员数: ' + members.length);
    if (members.length > 0) {
        print('成员列表:');
        members.forEach(function(m, i) {
            print('  ' + (i+1) + '. ' + m.username + ' (groups: ' + JSON.stringify(m.permission_groups) + ')');
        });
    }
}
"
echo ""

echo "=========================================="
echo "检查完成"
echo "=========================================="
echo ""
echo "如果发现问题，运行以下命令修复:"
echo "  go run scripts/diagnose_group_members.go  # 详细诊断"
echo "  go run scripts/fix_group_members.go       # 自动修复"
