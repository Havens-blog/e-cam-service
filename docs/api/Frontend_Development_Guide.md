# 前端开发指南

## 概述

本文档为前端开发人员提供多云 IAM 统一管理系统的开发指南。

## 技术栈建议

- **框架**: React / Vue 3
- **状态管理**: Redux / Pinia
- **HTTP 客户端**: Axios
- **UI 组件库**: Ant Design / Element Plus
- **路由**: React Router / Vue Router

## 页面结构

### 1. 用户管理页面

**路由**: `/iam/users`

**功能模块**:

- 用户列表（表格展示）
- 筛选条件（云厂商、用户类型、状态）
- 搜索框（用户名/邮箱）
- 创建用户按钮
- 批量操作（批量分配权限组、批量同步）
- 用户详情抽屉/弹窗

**关键组件**:

```jsx
<UserList>
  <UserFilter />
  <UserTable>
    <UserRow />
  </UserTable>
  <UserDetailDrawer />
  <CreateUserModal />
</UserList>
```

**API 调用**:

- `GET /api/v1/cam/iam/users` - 列表
- `POST /api/v1/cam/iam/users` - 创建
- `GET /api/v1/cam/iam/users/{id}` - 详情
- `PUT /api/v1/cam/iam/users/{id}` - 更新
- `DELETE /api/v1/cam/iam/users/{id}` - 删除

### 2. 权限组管理页面

**路由**: `/iam/groups`

**功能模块**:

- 权限组列表
- 创建权限组
- 编辑权限组
- 查看权限组成员
- 策略选择器（支持多云平台）

**关键组件**:

```jsx
<GroupList>
  <GroupCard />
  <CreateGroupModal>
    <PolicySelector />
    <PlatformSelector />
  </CreateGroupModal>
  <GroupMembersDrawer />
</GroupList>
```

**API 调用**:

- `GET /api/v1/cam/iam/groups` - 列表
- `POST /api/v1/cam/iam/groups` - 创建
- `GET /api/v1/cam/iam/groups/{id}` - 详情
- `GET /api/v1/cam/iam/groups/{id}/users` - 成员列表
- `GET /api/v1/cam/iam/policies` - 可用策略

### 3. 同步任务页面

**路由**: `/iam/sync`

**功能模块**:

- 任务列表（实时状态更新）
- 任务筛选（状态、云厂商）
- 创建同步任务
- 任务详情（进度、日志）
- 批量同步

**关键组件**:

```jsx
<SyncTaskList>
  <TaskFilter />
  <TaskTable>
    <TaskStatusBadge />
    <TaskProgress />
  </TaskTable>
  <TaskDetailModal />
  <BatchSyncModal />
</SyncTaskList>
```

**API 调用**:

- `GET /api/v1/cam/iam/sync/tasks` - 列表
- `POST /api/v1/cam/iam/sync/tasks` - 创建
- `GET /api/v1/cam/iam/sync/tasks/{id}` - 详情
- `POST /api/v1/cam/iam/sync/tasks/{id}/cancel` - 取消

### 4. 审计日志页面

**路由**: `/iam/audit`

**功能模块**:

- 日志列表（时间线展示）
- 高级筛选（操作类型、操作人、时间范围）
- 日志详情
- 导出功能
- 统计图表

**关键组件**:

```jsx
<AuditLogList>
  <LogFilter>
    <DateRangePicker />
    <OperationTypeSelect />
  </LogFilter>
  <LogTimeline />
  <LogDetailModal />
  <ExportButton />
  <StatisticsCharts />
</AuditLogList>
```

**API 调用**:

- `GET /api/v1/cam/iam/audit/logs` - 列表
- `GET /api/v1/cam/iam/audit/logs/{id}` - 详情
- `POST /api/v1/cam/iam/audit/logs/export` - 导出
- `GET /api/v1/cam/iam/audit/statistics` - 统计

### 5. 策略模板页面

**路由**: `/iam/templates`

**功能模块**:

- 模板列表（卡片展示）
- 模板分类（只读、读写、管理员、自定义）
- 创建模板
- 从权限组创建模板
- 应用模板到权限组

**关键组件**:

```jsx
<TemplateList>
  <CategoryTabs />
  <TemplateCard />
  <CreateTemplateModal />
  <ApplyTemplateModal />
</TemplateList>
```

**API 调用**:

- `GET /api/v1/cam/iam/templates` - 列表
- `POST /api/v1/cam/iam/templates` - 创建
- `POST /api/v1/cam/iam/templates/from-group` - 从权限组创建
- `POST /api/v1/cam/iam/templates/{id}/apply` - 应用模板

## 状态管理

### 用户状态

```typescript
interface UserState {
  users: User[];
  currentUser: User | null;
  loading: boolean;
  filters: UserFilters;
  pagination: Pagination;
}
```

### 权限组状态

```typescript
interface GroupState {
  groups: Group[];
  currentGroup: Group | null;
  availablePolicies: Policy[];
  loading: boolean;
}
```

### 同步任务状态

```typescript
interface SyncState {
  tasks: SyncTask[];
  runningTasks: SyncTask[];
  statistics: SyncStatistics;
  loading: boolean;
}
```

## HTTP 请求封装

### Axios 配置

```typescript
import axios from "axios";

const apiClient = axios.create({
  baseURL: "/api/v1/cam/iam",
  timeout: 30000,
  headers: {
    "Content-Type": "application/json",
  },
});

// 请求拦截器
apiClient.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem("token");
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => Promise.reject(error)
);

// 响应拦截器
apiClient.interceptors.response.use(
  (response) => {
    const { code, message, data } = response.data;
    if (code !== 0) {
      throw new Error(message);
    }
    return data;
  },
  (error) => {
    // 错误处理
    if (error.response?.status === 401) {
      // 跳转登录
    }
    return Promise.reject(error);
  }
);

export default apiClient;
```

### API 服务封装

```typescript
// services/userService.ts
import apiClient from "./apiClient";

export const userService = {
  // 获取用户列表
  getUsers: (params: UserListParams) => {
    return apiClient.get("/users", { params });
  },

  // 创建用户
  createUser: (data: CreateUserRequest) => {
    return apiClient.post("/users", data);
  },

  // 获取用户详情
  getUser: (id: number) => {
    return apiClient.get(`/users/${id}`);
  },

  // 更新用户
  updateUser: (id: number, data: UpdateUserRequest) => {
    return apiClient.put(`/users/${id}`, data);
  },

  // 删除用户
  deleteUser: (id: number) => {
    return apiClient.delete(`/users/${id}`);
  },

  // 批量分配权限组
  batchAssignGroups: (data: BatchAssignRequest) => {
    return apiClient.post("/users/batch-assign", data);
  },
};
```

## 类型定义

```typescript
// types/user.ts
export interface User {
  id: number;
  username: string;
  user_type: CloudUserType;
  cloud_account_id: number;
  provider: CloudProvider;
  cloud_user_id: string;
  display_name: string;
  email: string;
  permission_groups: number[];
  metadata: UserMetadata;
  status: CloudUserStatus;
  tenant_id: string;
  create_time: string;
  update_time: string;
}

export interface UserMetadata {
  last_login_time?: string;
  last_sync_time?: string;
  access_key_count: number;
  mfa_enabled: boolean;
  tags: Record<string, string>;
}

export type CloudProvider =
  | "aliyun"
  | "aws"
  | "azure"
  | "tencent"
  | "huawei"
  | "volcano";
export type CloudUserType = "api_key" | "access_key" | "ram_user" | "iam_user";
export type CloudUserStatus = "active" | "inactive" | "deleted";
```

## 实时更新

### WebSocket 连接（可选）

对于同步任务状态，建议使用 WebSocket 实现实时更新：

```typescript
const ws = new WebSocket("ws://localhost:8080/ws/sync");

ws.onmessage = (event) => {
  const task = JSON.parse(event.data);
  // 更新任务状态
  updateTaskStatus(task);
};
```

### 轮询方案

如果不使用 WebSocket，可以使用轮询：

```typescript
const pollTaskStatus = (taskId: number) => {
  const interval = setInterval(async () => {
    const task = await syncService.getTask(taskId);
    if (task.status === "success" || task.status === "failed") {
      clearInterval(interval);
    }
    updateTaskStatus(task);
  }, 3000); // 每3秒轮询一次
};
```

## 错误处理

### 统一错误提示

```typescript
const handleError = (error: any) => {
  if (error.response) {
    const { code, message } = error.response.data;
    switch (code) {
      case 1001:
        showError("云账号不存在");
        break;
      case 1002:
        showError("云账号凭证无效");
        break;
      default:
        showError(message || "操作失败");
    }
  } else {
    showError("网络错误，请稍后重试");
  }
};
```

## 性能优化

### 1. 列表虚拟滚动

对于大量数据的列表，使用虚拟滚动：

```jsx
import { FixedSizeList } from "react-window";

<FixedSizeList height={600} itemCount={users.length} itemSize={50}>
  {UserRow}
</FixedSizeList>;
```

### 2. 防抖搜索

```typescript
import { debounce } from "lodash";

const handleSearch = debounce((keyword: string) => {
  fetchUsers({ keyword });
}, 500);
```

### 3. 分页加载

```typescript
const loadMore = () => {
  if (hasMore && !loading) {
    fetchUsers({
      page: currentPage + 1,
      size: 20,
    });
  }
};
```

## 测试建议

### 单元测试

```typescript
describe("UserService", () => {
  it("should fetch users successfully", async () => {
    const users = await userService.getUsers({});
    expect(users).toBeDefined();
    expect(Array.isArray(users.list)).toBe(true);
  });
});
```

### E2E 测试

使用 Cypress 或 Playwright 进行端到端测试。

## 部署配置

### 环境变量

```env
VITE_API_BASE_URL=https://api.example.com
VITE_WS_URL=wss://api.example.com/ws
```

### Nginx 配置

```nginx
location /api/ {
    proxy_pass http://backend:8080/api/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

## 常见问题

### Q1: 如何处理多云平台的差异？

A: 使用统一的数据模型，在前端展示时根据 `provider` 字段显示不同的图标和标签。

### Q2: 同步任务如何实时更新？

A: 推荐使用 WebSocket，备选方案是轮询。

### Q3: 如何优化大量数据的渲染？

A: 使用虚拟滚动、分页加载、懒加载等技术。

## 联系方式

如有问题，请联系后端团队或查阅完整 API 文档。
