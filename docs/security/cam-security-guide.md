# CAM 安全配置指南

## 1. 云账号密钥安全

### 1.1 密钥加密存储
```go
// 使用 AES-256-GCM 加密存储敏感信息
type CloudAccountSecurity struct {
    EncryptionKey []byte
    cipher        cipher.AEAD
}

func (s *CloudAccountSecurity) EncryptSecret(plaintext string) (string, error) {
    nonce := make([]byte, s.cipher.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }
    
    ciphertext := s.cipher.Seal(nonce, nonce, []byte(plaintext), nil)
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *CloudAccountSecurity) DecryptSecret(ciphertext string) (string, error) {
    data, err := base64.StdEncoding.DecodeString(ciphertext)
    if err != nil {
        return "", err
    }
    
    nonceSize := s.cipher.NonceSize()
    if len(data) < nonceSize {
        return "", errors.New("ciphertext too short")
    }
    
    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := s.cipher.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", err
    }
    
    return string(plaintext), nil
}
```

### 1.2 密钥轮换策略
- 定期轮换云账号访问密钥（建议90天）
- 支持密钥版本管理，平滑切换
- 记录密钥使用历史和轮换日志

### 1.3 最小权限原则
```yaml
# 阿里云 RAM 策略示例 - 只读权限
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ecs:Describe*",
        "rds:Describe*",
        "oss:GetBucket*",
        "slb:Describe*",
        "vpc:Describe*"
      ],
      "Resource": "*"
    }
  ]
}
```

## 2. API 安全

### 2.1 认证和授权
```go
// JWT Token 验证中间件
func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token == "" {
            c.JSON(401, gin.H{"error": "missing authorization header"})
            c.Abort()
            return
        }
        
        // 验证 JWT Token
        claims, err := validateJWTToken(token)
        if err != nil {
            c.JSON(401, gin.H{"error": "invalid token"})
            c.Abort()
            return
        }
        
        // 检查权限
        if !hasPermission(claims.UserID, c.Request.URL.Path, c.Request.Method) {
            c.JSON(403, gin.H{"error": "insufficient permissions"})
            c.Abort()
            return
        }
        
        c.Set("user_id", claims.UserID)
        c.Next()
    }
}
```

### 2.2 输入验证
```go
// 云账号创建请求验证
type CreateCloudAccountRequest struct {
    Name            string `json:"name" binding:"required,min=1,max=100"`
    Provider        string `json:"provider" binding:"required,oneof=aliyun aws azure"`
    Environment     string `json:"environment" binding:"required,oneof=production staging development"`
    AccessKeyID     string `json:"access_key_id" binding:"required,min=16,max=128"`
    AccessKeySecret string `json:"access_key_secret" binding:"required,min=16,max=256"`
    Region          string `json:"region" binding:"required"`
    Description     string `json:"description" binding:"max=500"`
}

// 自定义验证器
func validateCloudProvider(fl validator.FieldLevel) bool {
    provider := fl.Field().String()
    validProviders := []string{"aliyun", "aws", "azure", "tencent", "huawei"}
    
    for _, valid := range validProviders {
        if provider == valid {
            return true
        }
    }
    return false
}
```

### 2.3 API 限流
```go
// 基于用户的限流
func RateLimitMiddleware() gin.HandlerFunc {
    limiter := rate.NewLimiter(rate.Every(time.Minute), 100) // 每分钟100次
    
    return func(c *gin.Context) {
        userID := c.GetString("user_id")
        
        if !limiter.Allow() {
            c.JSON(429, gin.H{
                "error": "rate limit exceeded",
                "retry_after": 60,
            })
            c.Abort()
            return
        }
        
        c.Next()
    }
}
```

## 3. 数据安全

### 3.1 敏感数据脱敏
```go
// 云账号信息脱敏
func (a *CloudAccount) MaskSensitiveData() *CloudAccountResponse {
    return &CloudAccountResponse{
        ID:          a.ID,
        Name:        a.Name,
        Provider:    a.Provider,
        Environment: a.Environment,
        AccessKeyID: maskAccessKey(a.AccessKeyID), // LTAI5t***VqFtAx1VBELjka
        Region:      a.Region,
        Status:      a.Status,
        CreateTime:  a.CreateTime,
        UpdateTime:  a.UpdateTime,
    }
}

func maskAccessKey(key string) string {
    if len(key) <= 8 {
        return strings.Repeat("*", len(key))
    }
    return key[:6] + strings.Repeat("*", len(key)-10) + key[len(key)-4:]
}
```

### 3.2 审计日志
```go
// 操作审计日志
type AuditLog struct {
    ID         int64     `json:"id"`
    UserID     string    `json:"user_id"`
    Action     string    `json:"action"`
    Resource   string    `json:"resource"`
    ResourceID string    `json:"resource_id"`
    IPAddress  string    `json:"ip_address"`
    UserAgent  string    `json:"user_agent"`
    Result     string    `json:"result"` // success, failed
    ErrorMsg   string    `json:"error_msg,omitempty"`
    Timestamp  time.Time `json:"timestamp"`
}

// 审计日志中间件
func AuditMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        
        // 记录请求信息
        auditLog := &AuditLog{
            UserID:    c.GetString("user_id"),
            Action:    c.Request.Method,
            Resource:  c.Request.URL.Path,
            IPAddress: c.ClientIP(),
            UserAgent: c.Request.UserAgent(),
            Timestamp: start,
        }
        
        c.Next()
        
        // 记录响应结果
        if c.Writer.Status() >= 400 {
            auditLog.Result = "failed"
            if err, exists := c.Get("error"); exists {
                auditLog.ErrorMsg = err.(error).Error()
            }
        } else {
            auditLog.Result = "success"
        }
        
        // 异步写入审计日志
        go writeAuditLog(auditLog)
    }
}
```

## 4. 网络安全

### 4.1 HTTPS 强制
```go
// HTTPS 重定向中间件
func HTTPSRedirectMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Header.Get("X-Forwarded-Proto") != "https" {
            httpsURL := "https://" + c.Request.Host + c.Request.RequestURI
            c.Redirect(301, httpsURL)
            c.Abort()
            return
        }
        c.Next()
    }
}
```

### 4.2 CORS 安全配置
```go
func CORSMiddleware() gin.HandlerFunc {
    return cors.New(cors.Config{
        AllowOrigins:     []string{"https://your-frontend-domain.com"},
        AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
        ExposeHeaders:    []string{"Content-Length"},
        AllowCredentials: true,
        MaxAge:           12 * time.Hour,
    })
}
```

## 5. 配置安全

### 5.1 环境变量配置
```bash
# 生产环境配置
export CAM_DB_PASSWORD="your-secure-db-password"
export CAM_JWT_SECRET="your-jwt-secret-key"
export CAM_ENCRYPTION_KEY="your-32-byte-encryption-key"
export CAM_REDIS_PASSWORD="your-redis-password"
```

### 5.2 配置文件安全
```yaml
# config/prod.yaml
security:
  encryption:
    algorithm: "AES-256-GCM"
    key_rotation_days: 90
  
  jwt:
    secret: "${CAM_JWT_SECRET}"
    expire_hours: 24
    
  rate_limit:
    requests_per_minute: 100
    burst_size: 10
    
  audit:
    enabled: true
    retention_days: 365
    
database:
  mongodb:
    password: "${CAM_DB_PASSWORD}"
    ssl: true
    auth_source: "admin"
```

## 6. 监控和告警

### 6.1 安全事件监控
```go
// 安全事件类型
const (
    SecurityEventLoginFailed     = "login_failed"
    SecurityEventUnauthorized    = "unauthorized_access"
    SecurityEventRateLimitHit    = "rate_limit_exceeded"
    SecurityEventSuspiciousAPI   = "suspicious_api_call"
)

// 安全事件告警
func alertSecurityEvent(eventType, userID, details string) {
    alert := SecurityAlert{
        Type:      eventType,
        UserID:    userID,
        Details:   details,
        Timestamp: time.Now(),
        Severity:  getSeverityLevel(eventType),
    }
    
    // 发送到监控系统
    go sendToMonitoring(alert)
    
    // 高危事件立即通知
    if alert.Severity == "high" {
        go sendImmediateAlert(alert)
    }
}
```

### 6.2 异常行为检测
```go
// 检测异常 API 调用模式
func detectAnomalousActivity(userID string, endpoint string) bool {
    // 检查调用频率
    if isHighFrequency(userID, endpoint) {
        alertSecurityEvent(SecurityEventSuspiciousAPI, userID, 
            fmt.Sprintf("High frequency calls to %s", endpoint))
        return true
    }
    
    // 检查异常时间访问
    if isOffHoursAccess(userID) {
        alertSecurityEvent(SecurityEventSuspiciousAPI, userID, 
            "Off-hours API access detected")
        return true
    }
    
    return false
}
```

## 7. 安全检查清单

### 部署前检查
- [ ] 所有敏感配置使用环境变量
- [ ] 数据库连接使用 SSL/TLS
- [ ] API 启用 HTTPS
- [ ] 实施输入验证和输出编码
- [ ] 配置适当的 CORS 策略
- [ ] 启用审计日志
- [ ] 设置监控和告警

### 运行时检查
- [ ] 定期检查访问日志异常
- [ ] 监控 API 调用模式
- [ ] 检查未授权访问尝试
- [ ] 验证密钥轮换策略执行
- [ ] 审查用户权限分配
- [ ] 检查系统漏洞更新

### 定期安全审计
- [ ] 代码安全审计（每季度）
- [ ] 渗透测试（每半年）
- [ ] 依赖库安全扫描（每月）
- [ ] 配置安全检查（每月）
- [ ] 访问权限审查（每季度）