// MongoDB 初始化脚本
// 创建应用数据库和用户

// 切换到应用数据库
db = db.getSiblingDB('e_cam_service');

// 创建应用用户
db.createUser({
  user: 'e_cam_user',
  pwd: 'e_cam_password',
  roles: [
    {
      role: 'readWrite',
      db: 'e_cam_service'
    }
  ]
});

// 创建基础集合和索引
db.createCollection('endpoints');
db.createCollection('users');
db.createCollection('sessions');

// 为 endpoints 集合创建索引
db.endpoints.createIndex({ "name": 1 }, { unique: true });
db.endpoints.createIndex({ "url": 1 });
db.endpoints.createIndex({ "method": 1 });
db.endpoints.createIndex({ "created_at": 1 });
db.endpoints.createIndex({ "updated_at": 1 });

// 为 users 集合创建索引
db.users.createIndex({ "username": 1 }, { unique: true });
db.users.createIndex({ "email": 1 }, { unique: true });
db.users.createIndex({ "created_at": 1 });

// 为 sessions 集合创建索引
db.sessions.createIndex({ "session_id": 1 }, { unique: true });
db.sessions.createIndex({ "user_id": 1 });
db.sessions.createIndex({ "expires_at": 1 }, { expireAfterSeconds: 0 });

print('✅ MongoDB 初始化完成');
print('数据库: e_cam_service');
print('用户: e_cam_user');
print('集合: endpoints, users, sessions');
print('索引已创建完成');