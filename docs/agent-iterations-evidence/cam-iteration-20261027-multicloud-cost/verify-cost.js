/**
 * cam-iteration-20261027-multicloud-cost 浏览器验收脚本
 * 运行：node verify-cost.js
 * 前置：nginx:8888 + e-cam-service:8001 + e-cam-web:5173 均在运行
 * 证据：截图 + 断言结果写入 e-cam-service/docs/agent-iterations-evidence/
 */
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const BASE = 'http://localhost:8888';
const EVIDENCE = 'D:/Haven/e-cam-service/docs/agent-iterations-evidence/cam-iteration-20261027-multicloud-cost';
const results = [];
const consoleErrors = [];
const failedRequests = [];
const apiResponses = [];

function record(step, detail, verdict) {
  results.push({ step, detail, verdict });
  console.log(`[${verdict}] ${step} :: ${detail}`);
}

(async () => {
  fs.mkdirSync(EVIDENCE, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();

  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 300));
  });
  page.on('requestfailed', (r) => {
    failedRequests.push(`${r.method()} ${r.url()} :: ${r.failure() && r.failure().errorText}`);
  });
  page.on('response', (r) => {
    if (r.url().includes('/api/') && r.status() >= 400) {
      failedRequests.push(`HTTP ${r.status()} ${r.url()}`);
    }
    if (r.url().includes('/api/v1/cam/cost/')) {
      apiResponses.push(`HTTP ${r.status()} ${r.url().replace(BASE, '')}`);
    }
  });

  // ---- 1. 未登录直达成本页：应跳统一登录 ----
  try {
    await page.goto(BASE + '/cam/finops/cost', { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.waitForTimeout(2500);
    const url1 = page.url();
    record('1.未登录访问成本页', '最终URL=' + url1, url1.includes('/console/login') ? 'PASS' : 'WARN');
    await page.screenshot({ path: path.join(EVIDENCE, '01-unauthed-redirect.png') });
  } catch (e) {
    record('1.未登录访问成本页', e.message.slice(0, 200), 'FAIL');
  }

  // ---- 2. 登录 admin / 12345678 ----
  try {
    await page.fill('[data-testid="username"]', 'admin');
    await page.fill('[data-testid="password"]', '12345678');
    await Promise.all([
      page.waitForLoadState('networkidle', { timeout: 30000 }).catch(() => {}),
      page.click('.login-form button.login-submit'),
    ]);
    await page.waitForTimeout(3000);
    const url2 = page.url();
    const body2 = await page.content();
    const needsMfa = body2.includes('mfa-code');
    const needsTenant = /tenant/i.test(url2);
    record('2.登录提交', '最终URL=' + url2 + (needsMfa ? ' [需MFA]' : '') + (needsTenant ? ' [需选租户]' : ''),
      needsMfa ? 'BLOCKED' : 'PASS');
    await page.screenshot({ path: path.join(EVIDENCE, '02-after-login.png'), fullPage: true });

    if (needsTenant) {
      // 截获租户选项文本供人工确认
      const texts = await page.locator('button, li, .tenant-option, [class*=tenant] span').allInnerTexts();
      fs.writeFileSync(path.join(EVIDENCE, 'tenant-options.txt'), texts.filter(t => t.trim()).join('\n'));
    }
  } catch (e) {
    record('2.登录提交', e.message.slice(0, 200), 'FAIL');
  }

  // ---- 3. 打开成本页（默认时间窗）----
  try {
    await page.goto(BASE + '/cam/finops/cost', { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.waitForTimeout(4000);
    record('3.打开成本页', 'URL=' + page.url(), page.url().includes('/finops/cost') ? 'PASS' : 'WARN');
    await page.screenshot({ path: path.join(EVIDENCE, '03-cost-default.png'), fullPage: true });
  } catch (e) {
    record('3.打开成本页', e.message.slice(0, 200), 'FAIL');
  }

  // ---- 4. 设定宽时间窗 2025-12-01 ~ 2026-10-27 ----
  try {
    const editor = page.locator('.el-range-editor').first();
    await editor.click();
    const inputs = page.locator('.el-range-editor input.el-range-input');
    await inputs.nth(0).fill('2025-12-01');
    await inputs.nth(1).fill('2026-10-27');
    await page.keyboard.press('Enter');
    await page.waitForTimeout(5000);
    record('4.设置日期范围', '2025-12-01 ~ 2026-10-27', 'DONE');
    await page.screenshot({ path: path.join(EVIDENCE, '04-cost-wide-window.png'), fullPage: true });
  } catch (e) {
    record('4.设置日期范围', e.message.slice(0, 200), 'FAIL');
  }

  // ---- 5. 断言页面出现四云（provider 维度分布图例/汇总卡）----
  try {
    const body = await page.content();
    for (const label of ['阿里云', '华为云', '腾讯云', '火山']) {
      record('5.宽窗口页面含「' + label + '」', body.includes(label) ? '出现' : '未出现',
        body.includes(label) ? 'FOUND' : 'MISS');
    }
  } catch (e) {
    record('5.四云标签断言', e.message.slice(0, 200), 'FAIL');
  }

  // ---- 6. 窄屏（375x812）关键操作可达性 ----
  try {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.waitForTimeout(2500);
    await page.screenshot({ path: path.join(EVIDENCE, '05-cost-mobile.png'), fullPage: true });
    const body = await page.content();
    record('6.窄屏渲染', '375x812 已截图', 'DONE');
  } catch (e) {
    record('6.窄屏渲染', e.message.slice(0, 200), 'FAIL');
  }

  // ---- 汇总 ----
  console.log('\n==== COST API 调用 ====');
  apiResponses.slice(0, 15).forEach((r) => console.log('  ' + r));
  console.log('==== 控制台错误 (' + consoleErrors.length + ') ====');
  consoleErrors.slice(0, 20).forEach((e) => console.log('  ERR: ' + e));
  console.log('==== 失败请求 (' + failedRequests.length + ') ====');
  failedRequests.slice(0, 20).forEach((e) => console.log('  REQ: ' + e));

  fs.writeFileSync(path.join(EVIDENCE, 'results.json'), JSON.stringify({ results, consoleErrors, failedRequests, apiResponses }, null, 2));
  await browser.close();
})();
