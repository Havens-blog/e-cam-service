# IAM API 快速参考

## 📋 完整 API 列表

### 用户管理

| 方法   | 路径                                 | 说明           |
| ------ | ------------------------------------ | -------------- |
| POST   | `/api/v1/cam/iam/users`              | 创建用户       |
| GET    | `/api/v1/cam/iam/users/{id}`         | 获取用户详情   |
| GET    | `/api/v1/cam/iam/users`              | 查询用户列表   |
| PUT    | `/api/v1/cam/iam/users/{id}`         | 更新用户       |
| DELETE | `/api/v1/cam/iam/users/{id}`         | 删除用户       |
| POST   | `/api/v1/cam/iam/users/sync`         | 同步用户       |
| POST   | `/api/v1/cam/iam/users/batch-assign` | 批量分配用户组 |

### 用户组管理

| 方法   | 路径                                   | 说明                |
| ------ | -------------------------------------- | ------------------- |
| POST   | `/api/v1/cam/iam/groups`               | 创建用户组          |
| GET    | `/api/v1/cam/iam/groups/{id}`          | 获取用户组详情      |
| GET    | `/api/v1/cam/iam/groups`               | 查询用户组列表      |
| PUT    | `/api/v1/cam/iam/groups/{id}`          | 更新用户组          |
| DELETE | `/api/v1/cam/iam/groups/{id}`          | 删除用户组          |
| PUT    | `/api/v1/cam/iam/groups/{id}/policies` | 更新权限策略        |
| POST   | `/api/v1/cam/iam/groups/sync`          | 同步用户组及成员 🆕 |
| GET    | `/api/v1/cam/iam/groups/{id}/members`  | 获取用户组成员 🆕   |

### 权限管理

| 方法 | 路径                                                    | 说明               |
| ---- | ------------------------------------------------------- | ------------------ |
| GET  | `/api/v1/cam/iam/permissions/users/{user_id}`           | 获取用户权限       |
| GET  | `/api/v1/cam/iam/permissions/users/{user_id}/effective` | 获取用户有效权限   |
| GET  | `/api/v1/cam/iam/permissions/groups/{group_id}`         | 获取用户组权限     |
| GET  | `/api/v1/cam/iam/permissions/policies`                  | 查询云平台权限策略 |

### 策略模板管理

| 方法   | 路径                                   | 说明             |
| ------ | -------------------------------------- | ---------------- |
| POST   | `/api/v1/cam/iam/templates`            | 创建策略模板     |
| GET    | `/api/v1/cam/iam/templates/{id}`       | 获取模板详情     |
| GET    | `/api/v1/cam/iam/templates`            | 查询模板列表     |
| PUT    | `/api/v1/cam/iam/templates/{id}`       | 更新模板         |
| DELETE | `/api/v1/cam/iam/templates/{id}`       | 删除模板         |
| POST   | `/api/v1/cam/iam/templates/from-group` | 从用户组创建模板 |

### 租户管理

| 方法   | 路径                                 | 说明         |
| ------ | ------------------------------------ | ------------ |
| POST   | `/api/v1/cam/iam/tenants`            | 创建租户     |
| GET    | `/api/v1/cam/iam/tenants/{id}`       | 获取租户详情 |
| GET    | `/api/v1/cam/iam/tenants`            | 查询租户列表 |
| PUT    | `/api/v1/cam/iam/tenants/{id}`       | 更新租户     |
| DELETE | `/api/v1/cam/iam/tenants/{id}`       | 删除租户     |
| GET    | `/api/v1/cam/iam/tenants/{id}/stats` | 获取租户统计 |

## 🚀 常用场景

### 场景 1: 查看用户完整信息

```bash
# 1. 获取用户基本信息
curl -X GET "http://localhost:8080/api/v1/cam/iam/users/1" \
  -H "X-Tenant-ID: tenant-001"

# 2. 获取用户权限
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/users/1/effective" \
  -H "X-Tenant-ID: tenant-001"
```

### 场景 2: 查看用户组完整信息

```bash
# 1. 获取用户组基本信息
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups/1" \
  -H "X-Tenant-ID: tenant-001"

# 2. 获取用户组成员
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups/1/members" \
  -H "X-Tenant-ID: tenant-001"

# 3. 获取用户组权限
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/groups/1" \
  -H "X-Tenant-ID: tenant-001"
```

### 场景 3: 同步云平台数据

```bash
# 1. 同步用户组及成员
curl -X POST "http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001"

# 2. 验证同步结果
curl -X GET "http://localhost:8080/api/v1/cam/iam/groups" \
  -H "X-Tenant-ID: tenant-001"

curl -X GET "http://localhost:8080/api/v1/cam/iam/users" \
  -H "X-Tenant-ID: tenant-001"
```

### 场景 4: 权限管理

```bash
# 1. 查询可用的权限策略
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/policies?cloud_account_id=1" \
  -H "X-Tenant-ID: tenant-001"

# 2. 更新用户组权限
curl -X PUT "http://localhost:8080/api/v1/cam/iam/groups/1/policies" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json" \
  -d '{
    "policies": [
      {
        "policy_id": "AliyunECSFullAccess",
        "policy_name": "AliyunECSFullAccess",
        "provider": "aliyun",
        "policy_type": "system"
      }
    ]
  }'

# 3. 验证权限更新
curl -X GET "http://localhost:8080/api/v1/cam/iam/permissions/groups/1" \
  -H "X-Tenant-ID: tenant-001"
```

## 📊 前端集成示例

### React 示例

```typescript
// API 服务
class IAMService {
  private baseURL = "http://localhost:8080/api/v1/cam/iam";
  private tenantID = "tenant-001";

  // 获取用户权限
  async getUserPermissions(userId: number) {
    const response = await fetch(
      `${this.baseURL}/permissions/users/${userId}`,
      {
        headers: {
          "X-Tenant-ID": this.tenantID,
        },
      }
    );
    return response.json();
  }

  // 获取用户组成员
  async getGroupMembers(groupId: number) {
    const response = await fetch(`${this.baseURL}/groups/${groupId}/members`, {
      headers: {
        "X-Tenant-ID": this.tenantID,
      },
    });
    return response.json();
  }

  // 同步用户组
  async syncGroups(cloudAccountId: number) {
    const response = await fetch(
      `${this.baseURL}/groups/sync?cloud_account_id=${cloudAccountId}`,
      {
        method: "POST",
        headers: {
          "X-Tenant-ID": this.tenantID,
        },
      }
    );
    return response.json();
  }
}

// 使用示例
const iamService = new IAMService();

// 获取用户权限
const permissions = await iamService.getUserPermissions(1);
console.log("用户权限:", permissions.data);

// 获取用户组成员
const members = await iamService.getGroupMembers(1);
console.log("用户组成员:", members.data);
```

### Vue 示例

```vue
<template>
  <div>
    <h2>用户权限</h2>
    <div v-if="loading">加载中...</div>
    <div v-else>
      <h3>所属用户组</h3>
      <ul>
        <li v-for="group in permissions.user_groups" :key="group.group_id">
          {{ group.display_name }}
          <ul>
            <li v-for="policy in group.policies" :key="policy.policy_id">
              {{ policy.policy_name }}
            </li>
          </ul>
        </li>
      </ul>
    </div>
  </div>
</template>

<script>
export default {
  data() {
    return {
      loading: true,
      permissions: null,
    };
  },
  async mounted() {
    await this.loadPermissions();
  },
  methods: {
    async loadPermissions() {
      try {
        const response = await fetch(
          `/api/v1/cam/iam/permissions/users/${this.userId}`,
          {
            headers: {
              "X-Tenant-ID": "tenant-001",
            },
          }
        );
        const data = await response.json();
        this.permissions = data.data;
      } catch (error) {
        console.error("加载权限失败:", error);
      } finally {
        this.loading = false;
      }
    },
  },
};
</script>
```

## 🔗 相关文档

- [用户组成员查询 API](GROUP_MEMBERS_API.md)
- [用户组成员同步功能](USER_GROUP_MEMBER_SYNC.md)
- [Swagger 文档更新说明](SWAGGER_UPDATED.md)
- [API 文档](API-DOCUMENTATION.md)

---

**更新日期**: 2025-11-25  
**版本**: v1.0
