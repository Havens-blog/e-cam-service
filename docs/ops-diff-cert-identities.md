# diff 域名证书身份清单（2026-11-14，TLS 实测）

**方法**：对 189 个 `diff` 域名逐个做 TLS 握手（SNI=域名），读对端 leaf 证书的 CN / 签发者 / 到期 / SAN，按「CN+签发者」聚成证书家族。比按指纹聚类更可执行——运维可直接按 CN 找到要换的那张证。

## 一、35 个证书家族（按影响域数排序）

| # | 线上证书 CN | 签发者 | 到期 | 影响域数 | 判断 |
|---|---|---|---|---|---|
| 1 | `design.jlcpcb.com`（多 SAN，Let's Encrypt）| Let's Encrypt | 2026-12-29 | **43** | 单张 LE 多域证书服务 43 个域；与台账不一致（可能是新入口未纳入台账，或台账仍登记旧证）|
| 2 | `*.jlc.com` | Sectigo | 2027-03-18 | **26** | 通配符与台账不符（多为 jlcops/jlcpcb 子域）|
| 3 | `*.jlcmes.com` | Sectigo | 2026-11-21 | 14 | 即将到期（7 天内），需换证 |
| 4 | `*.szlcmb.com` | DigiCert | 2027-04-30 | 10 | 与台账不符 |
| 5 | `synology`（群晖 NAS 自签）| Synology Inc. | 2027-04-24 | 9 | **NAS 自签证书对外暴露**，非台账证书 |
| 6 | `*.lcsccable.com` | DigiCert | 2027-04-30 | 7 | 与台账不符 |
| 7 | `*.oray.jlcops.com` | Sectigo | 2027-04-24 | 6 | 通配符已覆盖，但台账指纹是旧证 → 换证后未回写台账 |
| 8 | `*.oss-cn-shenzhen.aliyuncs.com` | GlobalSign | 2027-04-01 | 5 | OSS 区域通配证（= 四级域证书不覆盖类，见 #615）|
| 9 | **`*.jlcops.com`** | Sectigo | **2025-04-17** | 5 | ⚠️ **已过期 1.5 年** |
| 10 | `*.jlcpcb.com` | Sectigo | 2027-03-25 | 5 | 与台账不符 |
| 11 | `*.easyeda.com` | Sectigo | 2027-03-05 | 5 | 与台账不符 |
| 12 | `*.liveplay.myqcloud.com` | DigiCert | 2027-05-30 | 4 | 腾讯云直播默认域证书 |
| 13 | `*.tungsing-elec.hk` | Sectigo | 2027-04-01 | 4 | 与台账不符 |
| 14 | **`*.zxhgroup.com`** | Sectigo | **2026-07-01** | 4 | ⚠️ 已过期 |
| 15 | `*.exmail.qq.com` | DigiCert | 2027-02-04 | 3 | 腾讯企业邮域（验证/邮箱用途）|
| 16–35 | 其余 20 个家族 | — | — | 各 1–3 | 见 `diff-families.json` |

## 二、⚠️ 已过期但仍在服务的证书（7 张，17 个域名）

| CN | 签发者 | 到期 | 域名 |
|---|---|---|---|
| `*.jlcops.com` | Sectigo | **2025-04-17** | `gc1.jlcerp.com`、`gc2.jlcerp.com`、`td.jlcerp.com`、`td.jlcops.com`、`vpn.jlcops.com` |
| `*.zxhgroup.com` | Sectigo | **2026-07-01** | `test-dispatch.zxhwork.com`、`test-download.zxhwork.com`、`test-file-service-provider.zxhwork.com`、`test-puzzle-task.zxhwork.com` |
| `Kubernetes Ingress Controller Fake Certificate` | Acme Co（自签）| **2025-04-25** | `p2p.oray.jlcops.com`、`svr.oray.jlcops.com`、`rc.oray.jlcops.com` |
| `*.fa-part.com` | Sectigo | **2024-04-21** | `zxhdownload.jlcerp.com`、`zxhwip.jlcerp.com` |
| `*.jlcsmt.com` | Sectigo | 2026-08-12 | `canary.jlcsmt.com` |
| `count.jlcfa.com` | DigiCert | 2024-08-22 | `count.jlcfa.com` |
| `*.jlcerp.com` | Sectigo | 2025-08-16 | `test-3d-pan.jlcerp.com` |

**这三类最该立刻处理**：
1. `*.jlcops.com`（含 **vpn.jlcops.com**）—— VPN 入口用过期证书，客户端会直接告警/拒绝；
2. `Kubernetes Ingress Controller Fake Certificate`—— **K8s ingress 未配真实证书**（`p2p/svr/rc.oray.jlcops.com`），自签证书对公网暴露；
3. `synology` 自签证书服务 9 个 `*-nas.jlcmes.com`—— NAS 管理面直接暴露在公网。

## 三、6 个握手失败（需单独排查）

`app.jlc-work.com`、`canary.jlcdfm.com`（ENOTFOUND，DNS 已不存在）；`account.forface3d.net`、`jlccan.com`、`rs.jlc3dp.com`、`passport.jlcdmp.com`（timeout）。

## 四、数据落盘

- `D:\Haven\.browser-verify\cam-t5\diff-certs.json`（189 条逐域握手结果）
- `D:\Haven\.browser-verify\cam-t5\diff-families.json`（35 个家族聚合，含全部域名清单）

## 五、建议动作顺序

| 顺序 | 动作 | 影响 |
|---|---|---|
| 1 | 换 `*.jlcops.com`（含 VPN）| 5 域，安全风险最高 |
| 2 | K8s ingress 安装真实证书（oray 组）| 3 域，自签暴露 |
| 3 | 核实 `design.jlcpcb.com` LE 多域证书是否应纳入台账 | 43 域，一次对齐 |
| 4 | NAS 自签证书改内网或换正式证书 | 9 域 |
| 5 | 其余按家族换证并回写台账指纹 | ~110 域 |
