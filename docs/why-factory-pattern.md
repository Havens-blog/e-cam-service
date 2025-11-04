# 为什么需要适配器工厂？

## 场景对比

### 场景 1：资源同步服务

#### 没有工厂（代码重复）

```go
// internal/cam/service/sync_service.go

func (s *SyncService) SyncAccount(accountID int64) error {
    account, _ := s.repo.GetByID(accountID)

    // 😫 每个方法都要写这堆判断
    var adapter CloudAdapter
    if account.Provider == "aliyun" {
        adapter, _ = NewAliyunAdapter(account)
    } else if account.Provider == "aws" {
        adapter, _ = NewAWSAdapter(account)
    } else if account.Provider == "azure" {
        adapter, _ = NewAzureAdapter(account)
    } else {
        return errors.New("不支持")
    }

    instances, _ := adapter.GetECSInstances(ctx, region)
    // ...
}

func (s *SyncService) ValidateAccount(accountID int64) error {
    account, _ := s.repo.GetByID(accountID)

    // 😫 又要写一遍相同的判断
    var adapter CloudAdapter
    if account.Provider == "aliyun" {
        adapter, _ = NewAliyunAdapter(account)
    } else if account.Provider == "aws" {
        adapter, _ = NewAWSAdapter(account)
    } else if account.Provider == "azure" {
        adapter, _ = NewAzureAdapter(account)
    } else {
        return errors.New("不支持")
    }

    err := adapter.ValidateCredentials(ctx)
    // ...
}

func (s *SyncService) GetRegions(accountID int64) error {
    account, _ := s.repo.GetByID(accountID)

    // 😫 第三次写相同的判断
    var adapter CloudAdapter
    if account.Provider == "aliyun" {
        adapter, _ = NewAliyunAdapter(account)
    } else if account.Provider == "aws" {
        adapter, _ = NewAWSAdapter(account)
    } else if account.Provider == "azure" {
        adapter, _ = NewAzureAdapter(account)
    } else {
        return errors.New("不支持")
    }

    regions, _ := adapter.GetRegions(ctx)
    // ...
}

// 问题：
// 1. 三个方法都写了相同的判断逻辑（代码重复）
// 2. 新增腾讯云时，三个地方都要改
// 3. 如果有 10 个方法，就要写 10 次
```

#### 有工厂（简洁优雅）

```go
// internal/cam/service/sync_service.go

type SyncService struct {
    repo    Repository
    factory *adapter.AdapterFactory  // 注入工厂
}

func (s *SyncService) SyncAccount(accountID int64) error {
    account, _ := s.repo.GetByID(accountID)

    // ✅ 一行代码搞定
    adapter, _ := s.factory.CreateAdapter(account)

    instances, _ := adapter.GetECSInstances(ctx, region)
    // ...
}

func (s *SyncService) ValidateAccount(accountID int64) error {
    account, _ := s.repo.GetByID(accountID)

    // ✅ 一行代码搞定
    adapter, _ := s.factory.CreateAdapter(account)

    err := adapter.ValidateCredentials(ctx)
    // ...
}

func (s *SyncService) GetRegions(accountID int64) error {
    account, _ := s.repo.GetByID(accountID)

    // ✅ 一行代码搞定
    adapter, _ := s.factory.CreateAdapter(account)

    regions, _ := adapter.GetRegions(ctx)
    // ...
}

// 优势：
// 1. 每个方法只需要一行代码创建适配器
// 2. 新增腾讯云只需要改工厂，这三个方法不用动
// 3. 代码简洁，易于维护
```

### 场景 2：新增云厂商

#### 没有工厂（到处改代码）

```go
// 😫 需要改的地方：

// 1. sync_service.go - 改 3 个方法
func (s *SyncService) SyncAccount() {
    if account.Provider == "tencent" {  // 新增
        adapter, _ = NewTencentAdapter(account)
    }
}

// 2. cost_service.go - 改 2 个方法
func (s *CostService) CalculateCost() {
    if account.Provider == "tencent" {  // 新增
        adapter, _ = NewTencentAdapter(account)
    }
}

// 3. monitor_service.go - 改 4 个方法
func (s *MonitorService) CheckHealth() {
    if account.Provider == "tencent" {  // 新增
        adapter, _ = NewTencentAdapter(account)
    }
}

// 4. report_service.go - 改 2 个方法
// ...

// 总共需要改 11 个地方！容易漏改，容易出错
```

#### 有工厂（只改一个地方）

```go
// ✅ 只需要改工厂一个地方

// internal/cam/sync/adapter/factory.go
func (f *AdapterFactory) CreateAdapter(account *CloudAccount) (CloudAdapter, error) {
    switch account.Provider {
    case "aliyun":
        return NewAliyunAdapter(account)
    case "aws":
        return NewAWSAdapter(account)
    case "azure":
        return NewAzureAdapter(account)
    case "tencent":  // 只需要在这里加一行
        return NewTencentAdapter(account)
    default:
        return nil, errors.New("不支持")
    }
}

// 所有业务代码都不需要改！
// sync_service.go - 不用改
// cost_service.go - 不用改
// monitor_service.go - 不用改
// report_service.go - 不用改
```

## 工厂模式的本质

### 工厂做什么？

```go
// 工厂就是一个"创建对象的专家"
type AdapterFactory struct{}

func (f *AdapterFactory) CreateAdapter(account *CloudAccount) (CloudAdapter, error) {
    // 根据条件决定创建哪个具体的对象
    switch account.Provider {
    case "aliyun":
        return NewAliyunAdapter(account)
    case "aws":
        return NewAWSAdapter(account)
    // ...
    }
}
```

### 为什么需要这个"专家"？

1. **集中管理创建逻辑**

   - 所有创建逻辑都在工厂里
   - 业务代码不需要知道怎么创建

2. **降低耦合**

   - 业务代码只依赖工厂和接口
   - 不依赖具体的适配器实现

3. **易于扩展**
   - 新增类型只需要改工厂
   - 业务代码完全不受影响

## 实际项目中的使用

### 你的项目结构

```
internal/cam/
├── service/
│   ├── sync_service.go      # 使用工厂创建适配器
│   ├── cost_service.go      # 使用工厂创建适配器
│   └── monitor_service.go   # 使用工厂创建适配器
│
└── sync/
    └── adapter/
        ├── factory.go       # 工厂：统一创建适配器
        ├── aliyun_adapter.go
        ├── aws_adapter.go
        └── azure_adapter.go
```

### 依赖注入

```go
// internal/cam/wire.go

func InitSyncService(
    repo Repository,
    factory *adapter.AdapterFactory,  // 注入工厂
) *SyncService {
    return &SyncService{
        repo:    repo,
        factory: factory,
    }
}

// 使用时
func main() {
    factory := adapter.NewAdapterFactory()
    syncService := InitSyncService(repo, factory)

    // syncService 内部使用 factory 创建适配器
    syncService.SyncAccount(123)
}
```

## 类比：餐厅点餐

### 没有工厂（顾客自己做菜）

```go
// 顾客（业务代码）需要知道怎么做每道菜
func OrderFood(dishName string) {
    if dishName == "宫保鸡丁" {
        // 自己切鸡肉、炒花生、调酱汁...
    } else if dishName == "麻婆豆腐" {
        // 自己切豆腐、炒肉末、调麻辣酱...
    } else if dishName == "鱼香肉丝" {
        // 自己切肉丝、调鱼香汁...
    }
}
```

### 有工厂（厨房统一做菜）

```go
// 顾客（业务代码）只需要告诉厨房（工厂）要什么
func OrderFood(dishName string) {
    kitchen := NewKitchen()  // 工厂
    dish := kitchen.MakeDish(dishName)  // 厨房负责做菜
    eat(dish)
}

// 厨房（工厂）知道怎么做每道菜
type Kitchen struct{}

func (k *Kitchen) MakeDish(dishName string) Dish {
    switch dishName {
    case "宫保鸡丁":
        return makeKungPaoChicken()
    case "麻婆豆腐":
        return makeMapoTofu()
    case "鱼香肉丝":
        return makeYuxiangPork()
    }
}
```

## 总结

### 工厂模式解决的问题

1. **避免代码重复**：创建逻辑只写一次
2. **降低耦合**：业务代码不依赖具体实现
3. **易于扩展**：新增类型只改工厂
4. **统一管理**：创建逻辑集中在一个地方

### 简单记忆

- **适配器**：统一不同云厂商的 API 差异
- **工厂**：统一创建适配器的方式

**适配器解决"怎么用"的问题，工厂解决"怎么创建"的问题。**

### 你的项目中

```go
// 业务代码永远只需要这两行
factory := adapter.NewAdapterFactory()
adapter, _ := factory.CreateAdapter(account)

// 然后就可以用了
instances, _ := adapter.GetECSInstances(ctx, region)
```

**工厂让创建对象变得简单，业务代码不需要关心创建细节。**
