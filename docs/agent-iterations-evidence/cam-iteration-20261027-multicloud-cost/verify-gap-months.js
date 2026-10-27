/**
 * 缺口月份复验：窗口 2026-04-01 ~ 2026-09-30
 * 预期：阿里云 ✓ / 腾讯云 ✓（补采新增）/ 火山引擎 ✓（9月）/ 华为云 ✗（仅2月，精确性断言）
 */
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const BASE = 'http://localhost:8888';
const EVIDENCE = 'D:/Haven/e-cam-service/docs/agent-iterations-evidence/cam-iteration-20261027-multicloud-cost';

(async () => {
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();

  const distResponses = [];
  page.on('response', async (r) => {
    if (r.url().includes('cost/distribution') && r.url().includes('2026-04')) {
      try { distResponses.push({ url: r.url().replace(BASE, ''), body: await r.json() }); } catch (e) { /* ignore */ }
    }
  });

  // 登录
  await page.goto(BASE + '/console/login', { waitUntil: 'domcontentloaded' });
  await page.fill('[data-testid="username"]', 'admin');
  await page.fill('[data-testid="password"]', '12345678');
  await Promise.all([
    page.waitForLoadState('networkidle', { timeout: 30000 }).catch(() => {}),
    page.click('.login-form button.login-submit'),
  ]);
  await page.waitForTimeout(2500);

  // 成本页 + 缺口窗口
  await page.goto(BASE + '/cam/finops/cost', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(3000);
  const editor = page.locator('.el-range-editor').first();
  await editor.click();
  const inputs = page.locator('.el-range-editor input.el-range-input');
  await inputs.nth(0).fill('2026-04-01');
  await inputs.nth(1).fill('2026-09-30');
  await page.keyboard.press('Enter');
  await page.waitForTimeout(5000);

  await page.screenshot({ path: path.join(EVIDENCE, '06-cost-gap-months.png'), fullPage: true });
  const body = await page.content();

  console.log('==== 缺口窗口 (2026-04-01 ~ 2026-09-30) 断言 ====');
  for (const [label, expect] of [['阿里云', true], ['腾讯云', true], ['火山', true], ['华为云', false]]) {
    const found = body.includes(label);
    const verdict = found === expect ? 'PASS' : 'FAIL';
    console.log(`[${verdict}] ${label}: ${found ? '出现' : '未出现'}（预期${expect ? '出现' : '不出现'}）`);
  }

  console.log('\n==== distribution API 响应（缺口窗口）====');
  for (const d of distResponses) {
    console.log('  ' + d.url);
    if (d.body && d.body.data) {
      const items = Array.isArray(d.body.data) ? d.body.data : d.body.data.items || d.body.data.list || [];
      for (const it of items.slice(0, 8)) {
        console.log(`    ${JSON.stringify(it).slice(0, 160)}`);
      }
    }
  }

  fs.writeFileSync(path.join(EVIDENCE, 'gap-months-distribution.json'), JSON.stringify(distResponses, null, 2));
  await browser.close();
})();
