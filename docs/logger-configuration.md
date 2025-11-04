# 日志配置指南

## 概述

E-CAM Service 使用 Ego 框架的 elog 日志组件，基于 zap 实现高性能日志记录。

## 日志格式

### Console 格式（推荐用于开发和调试）

```
2025-11-04 10:30:45  INFO  caller=ioc/db.go:20  开始初始化MongoDB连接
2025-11-04 10:30:45  INFO  caller=ioc/db.go:88  MongoDB连接初始化完成  database=e_cam_service
2025-11-04 10:30:46  WARN  caller=service/account.go:45  云账号连接测试失败  account_id=123  error=timeout
2025-11-04 10:30:47  ERROR caller=web/handler.go:89  创建资产失败  error=invalid input
```

**格式说明**：

- `时间` - 精确到秒的时间戳
- `级别` - 大写的日志级别（INFO/WARN/ERROR/DEBUG）
- `caller` - 调用位置（文件名:行号）
- `消息` - 日志主要内容
- `字段` - 结构化的上下文信息（key=value）

### JSON 格式（推荐用于生产环境）

```json
{
  "time": "2025-11-04 10:30:45",
  "level": "INFO",
  "caller": "ioc/db.go:20",
  "msg": "开始初始化MongoDB连接"
}
```

## 配置文件

### 开发环境配置 (config/dev.yaml)

```yaml
logger:
  default:
    level: "debug" # 开发环境使用 debug 级别
    format: "console" # 使用 console 格式，易读
    enableCaller: true # 显示调用位置
    callerSkip: 2 # 跳过的调用栈层数
    enableStacktrace: false # 不显示堆栈（ERROR 级别除外）
    timeFormat: "2006-01-02 15:04:05"
    outputPaths:
      - "stdout" # 输出到控制台
      - "logs/dev.log" # 同时写入文件
    errorOutputPaths:
      - "stderr"
      - "logs/error.log"
    encoderConfig:
      timeKey: "time"
      levelKey: "level"
      nameKey: "logger"
      callerKey: "caller"
      messageKey: "msg"
      stacktraceKey: "stacktrace"
      levelEncoder: "capital" # 大写级别
      timeEncoder: "2006-01-02 15:04:05"
      durationEncoder: "string"
      callerEncoder: "short" # 短路径
```

### 生产环境配置 (config/prod.yaml)

```yaml
logger:
  default:
    level: "info" # 生产环境使用 info 级别
    format: "json" # 使用 JSON 格式，便于日志收集
    enableCaller: true
    callerSkip: 2
    enableStacktrace: true # 生产环境启用堆栈跟踪
    timeFormat: "2006-01-02 15:04:05"
    outputPaths:
      - "stdout"
      - "/var/log/e-cam-service/app.log"
    errorOutputPaths:
      - "stderr"
      - "/var/log/e-cam-service/error.log"
    encoderConfig:
      timeKey: "time"
      levelKey: "level"
      nameKey: "logger"
      callerKey: "caller"
      messageKey: "msg"
      stacktraceKey: "stacktrace"
      levelEncoder: "capital"
      timeEncoder: "2006-01-02 15:04:05"
      durationEncoder: "string"
      callerEncoder: "short"
```

## 日志级别

| 级别  | 说明     | 使用场景                                 |
| ----- | -------- | ---------------------------------------- |
| DEBUG | 调试信息 | 详细的程序执行信息，仅用于开发调试       |
| INFO  | 一般信息 | 重要的业务流程节点，如服务启动、连接建立 |
| WARN  | 警告信息 | 潜在问题，但不影响系统运行               |
| ERROR | 错误信息 | 错误情况，需要关注和处理                 |
| FATAL | 致命错误 | 严重错误，导致程序无法继续运行           |

## 使用示例

### 基本日志记录

```go
import "github.com/gotomicro/ego/core/elog"

func main() {
    logger := elog.DefaultLogger

    // INFO 级别
    logger.Info("服务启动成功",
        elog.String("version", "1.0.0"),
        elog.Int("port", 8001))

    // WARN 级别
    logger.Warn("配置项缺失，使用默认值",
        elog.String("key", "timeout"),
        elog.Any("default", 30))

    // ERROR 级别
    logger.Error("数据库连接失败",
        elog.FieldErr(err),
        elog.String("dsn", "mongodb://..."))
}
```

### 结构化日志字段

```go
// 字符串字段
elog.String("user_id", "12345")

// 整数字段
elog.Int("count", 100)
elog.Int64("timestamp", time.Now().Unix())

// 浮点数字段
elog.Float64("price", 99.99)

// 布尔字段
elog.Bool("success", true)

// 错误字段
elog.FieldErr(err)

// 任意类型字段
elog.Any("data", complexObject)

// 时间字段
elog.Time("created_at", time.Now())

// 持续时间字段
elog.Duration("elapsed", duration)
```

### 上下文日志

```go
func ProcessRequest(ctx context.Context, req *Request) error {
    logger := elog.DefaultLogger

    // 记录请求开始
    logger.Info("开始处理请求",
        elog.String("request_id", req.ID),
        elog.String("user_id", req.UserID),
        elog.String("action", req.Action))

    // 业务逻辑
    result, err := doSomething(ctx, req)
    if err != nil {
        logger.Error("处理请求失败",
            elog.String("request_id", req.ID),
            elog.FieldErr(err))
        return err
    }

    // 记录请求完成
    logger.Info("请求处理完成",
        elog.String("request_id", req.ID),
        elog.Any("result", result))

    return nil
}
```

### 性能日志

```go
func SlowOperation() {
    logger := elog.DefaultLogger
    start := time.Now()

    // 执行操作
    doWork()

    elapsed := time.Since(start)

    // 记录执行时间
    logger.Info("操作完成",
        elog.String("operation", "slow_operation"),
        elog.Duration("elapsed", elapsed))

    // 如果执行时间过长，记录警告
    if elapsed > 5*time.Second {
        logger.Warn("操作执行时间过长",
            elog.String("operation", "slow_operation"),
            elog.Duration("elapsed", elapsed),
            elog.Duration("threshold", 5*time.Second))
    }
}
```

## 日志轮转

### 使用 logrotate（Linux）

创建配置文件 `/etc/logrotate.d/e-cam-service`：

```
/var/log/e-cam-service/*.log {
    daily                   # 每天轮转
    rotate 30               # 保留30天
    compress                # 压缩旧日志
    delaycompress           # 延迟压缩
    missingok               # 文件不存在不报错
    notifempty              # 空文件不轮转
    create 0644 ecam ecam   # 创建新文件的权限和所有者
    sharedscripts
    postrotate
        # 重新加载服务（如果需要）
        systemctl reload e-cam-service > /dev/null 2>&1 || true
    endscript
}
```

### 使用 lumberjack（代码实现）

```go
import "gopkg.in/natefinch/lumberjack.v2"

func InitLogger() {
    logger := &lumberjack.Logger{
        Filename:   "/var/log/e-cam-service/app.log",
        MaxSize:    100,  // MB
        MaxBackups: 30,   // 保留文件数
        MaxAge:     30,   // 天
        Compress:   true, // 压缩
    }

    // 配置 zap 使用 lumberjack
    // ...
}
```

## 日志监控

### 使用 ELK Stack

1. **Filebeat** 收集日志文件
2. **Logstash** 解析和转换日志
3. **Elasticsearch** 存储日志
4. **Kibana** 可视化分析

Filebeat 配置示例：

```yaml
filebeat.inputs:
  - type: log
    enabled: true
    paths:
      - /var/log/e-cam-service/*.log
    json.keys_under_root: true
    json.add_error_key: true

output.elasticsearch:
  hosts: ["localhost:9200"]
  index: "e-cam-service-%{+yyyy.MM.dd}"
```

### 使用 Grafana Loki

```yaml
# promtail 配置
server:
  http_listen_port: 9080

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  - job_name: e-cam-service
    static_configs:
      - targets:
          - localhost
        labels:
          job: e-cam-service
          __path__: /var/log/e-cam-service/*.log
```

## 最佳实践

### 1. 日志级别选择

- **DEBUG**: 仅在开发环境使用，记录详细的执行流程
- **INFO**: 记录重要的业务节点和状态变化
- **WARN**: 记录潜在问题，但不影响主流程
- **ERROR**: 记录错误情况，需要人工介入

### 2. 结构化日志

始终使用结构化字段，而不是字符串拼接：

```go
// ❌ 不推荐
logger.Info(fmt.Sprintf("用户 %s 登录成功，IP: %s", userID, ip))

// ✅ 推荐
logger.Info("用户登录成功",
    elog.String("user_id", userID),
    elog.String("ip", ip))
```

### 3. 敏感信息脱敏

```go
// 脱敏密码
logger.Info("用户登录",
    elog.String("username", username),
    elog.String("password", "***"))  // 不记录真实密码

// 脱敏 AK/SK
logger.Info("云账号配置",
    elog.String("access_key", maskString(ak)),
    elog.String("secret_key", "***"))
```

### 4. 性能考虑

```go
// ❌ 避免在循环中频繁记录
for _, item := range items {
    logger.Debug("处理项目", elog.Any("item", item))
}

// ✅ 批量记录或降低频率
logger.Info("开始批量处理", elog.Int("count", len(items)))
// 处理逻辑
logger.Info("批量处理完成", elog.Int("success", successCount))
```

### 5. 错误上下文

```go
// ✅ 记录完整的错误上下文
if err := service.CreateAsset(ctx, asset); err != nil {
    logger.Error("创建资产失败",
        elog.String("asset_id", asset.ID),
        elog.String("asset_name", asset.Name),
        elog.String("provider", asset.Provider),
        elog.FieldErr(err))
    return err
}
```

## 故障排查

### 查看实时日志

```bash
# 查看所有日志
tail -f logs/app.log

# 只查看错误日志
tail -f logs/error.log

# 过滤特定关键字
tail -f logs/app.log | grep "ERROR"

# 查看最近的错误
grep "ERROR" logs/app.log | tail -20
```

### 日志分析

```bash
# 统计错误数量
grep "ERROR" logs/app.log | wc -l

# 按错误类型分组
grep "ERROR" logs/app.log | awk '{print $5}' | sort | uniq -c

# 查找特定时间段的日志
grep "2025-11-04 10:" logs/app.log

# 查找特定用户的操作
grep "user_id=12345" logs/app.log
```

## 测试日志配置

运行测试脚本验证日志配置：

```bash
go run scripts/test_simple_logger.go
```

检查输出文件：

- `logs/test.log` - 所有级别的日志
- `logs/test_error.log` - 仅错误日志

## 参考资料

- [Ego 日志组件文档](https://ego.gocn.vip/frame/core/elog.html)
- [Zap 日志库](https://github.com/uber-go/zap)
- [日志最佳实践](https://12factor.net/logs)
