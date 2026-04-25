// MongoDB 初始化脚本（docker-compose 首次启动时执行）
// 创建 ecam 数据库和应用用户

db = db.getSiblingDB('ecam');

db.createUser({
  user: 'ecam',
  pwd: 'ecam123',
  roles: [
    { role: 'readWrite', db: 'ecam' }
  ]
});

print('✅ MongoDB 初始化完成');
print('  数据库: ecam');
print('  用户: ecam');
