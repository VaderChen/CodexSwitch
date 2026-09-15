// Run with Playwright available: node --test src/tests/tooltips.browser.cjs
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require('playwright');
const web=path.join(__dirname,'../web');
const html=fs.readFileSync(path.join(web,'index.html'),'utf8')
 .replace('/* STYLE_PLACEHOLDER */',fs.readFileSync(path.join(web,'style.css'),'utf8'))
 .replace('<!-- APP_ICON -->',fs.readFileSync(path.join(__dirname,'../assets/CodexSwitch.png')).toString('base64'));

test('用量泡泡限帳號資訊區；按鈕、間隙與背景更新不觸發',async()=>{
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'msedge',headless:true});
 try{
  const page=await browser.newPage();
  const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>route.abort());
  await page.setContent(html.replace('<script>',`<script>
   window.getAccounts=async()=>[
    {id:'sample',email:'sample@example.com',updated_at:'2026-09-15T12:00:00Z'},
    {id:'current',email:'current@example.com',is_current:true}
   ];
   window.getAccountUsage=async()=>({sample:{windows:[{seconds:18000,remaining:52},{seconds:604800,remaining:73}]}});
   window.savedHintPreferences=[];
   window.saveButtonHints=async value=>window.savedHintPreferences.push(value);
  </script><script>`));
  await page.waitForFunction(()=>document.querySelectorAll('.account').length===2);
  const tip=page.locator('#accountTooltip');
  const buttonTip=page.locator('#buttonTooltip');
  const first=page.locator('.account').first();
  assert.equal(await page.locator('button[title]').count(),0,'按鈕不可同時顯示原生 title 提示');
  await first.locator('.account-settings').hover();
  assert.match(await buttonTip.textContent(),/^Codex 環境設定：.*CODEX_HOME.*USER_DATA_DIR/);
  assert.equal(await page.locator('#settingsDialog h3').textContent(),'Codex 環境設定');
  await page.evaluate(()=>openAbout());
  await page.locator('#buttonHintsToggle').uncheck();
  await page.evaluate(()=>closeAbout());
  await first.locator('.reset-account').hover();
  assert.equal(await buttonTip.isVisible(),false,'關閉開關後不顯示按鈕提示');
  await first.locator('strong').hover();
  assert.equal(await tip.isVisible(),true,'關閉按鈕提示不影響用量泡泡');
  await page.evaluate(()=>openAbout());
  assert.equal(await page.locator('#buttonHintsToggle').isChecked(),false);
  await page.locator('#buttonHintsToggle').check();
  await page.evaluate(()=>closeAbout());
  await first.locator('.reset-account').hover();
  assert.equal(await buttonTip.isVisible(),true,'重新啟用後恢復提示');
  assert.deepEqual(await page.evaluate(()=>window.savedHintPreferences),[false,true]);
  const gap=async row=>{
   const reset=await row.locator('.reset-account').boundingBox();
   const apply=await row.locator('.use-account:not(.reset-account)').boundingBox();
   const x=(reset.x+reset.width+apply.x)/2,y=reset.y+reset.height/2;
   assert.equal(await page.evaluate(({x,y})=>document.elementFromPoint(x,y).classList.contains('account'),{x,y}),true);
   await page.mouse.move(x,y);
  };
  for(const viewport of [{width:760,height:650},{width:1100,height:780}]){
   await page.setViewportSize(viewport);
   await page.mouse.move(1,1);
   await gap(first);
   assert.equal(await tip.isVisible(),false,'直接進入重置與套用的間隙，不可顯示用量');
   await first.locator('strong').hover();
   assert.equal(await tip.isVisible(),true,'帳號文字顯示用量');
   assert.match(await tip.textContent(),/52%/);
   await first.locator('.account-usage').hover();
   assert.equal(await tip.isVisible(),true,'帳號下方資訊仍顯示用量');
   await first.locator('.reset-account').hover();
   assert.equal(await tip.isVisible(),false,'重置按鈕不顯示用量');
   assert.equal(await buttonTip.textContent(),'搜尋該帳號之重置卷、在列表中長按後可重置額度');
   await gap(first);
   assert.equal(await tip.isVisible(),false,'從重置移到間隙，不可顯示用量');
   assert.equal(await buttonTip.isVisible(),false,'間隙不顯示按鈕泡泡');
   await page.evaluate(()=>refreshUsage());
   assert.equal(await tip.isVisible(),false,'背景用量更新不可讓泡泡重現');
   await first.locator('strong').hover();
   await gap(first);
   assert.equal(await tip.isVisible(),false,'從帳號直接移到間隙即隱藏');
   for(const selector of ['.connected','.account-settings','.use-account:not(.reset-account)','.delete']){
    await first.locator('strong').hover();
    await first.locator(selector).hover();
    assert.equal(await tip.isVisible(),false,selector+' 不可顯示用量');
   }
   const rowBox=await first.boundingBox();
   await page.mouse.move(rowBox.x+2,rowBox.y+2);
   assert.equal(await tip.isVisible(),false,'列邊緣空白不可顯示用量');
   const current=page.locator('.account').nth(1);
   await current.locator('strong').hover();
   await current.locator('button:disabled').hover();
   assert.equal(await tip.isVisible(),false,'停用的套用按鈕不可顯示用量');
   await gap(current);
   assert.equal(await tip.isVisible(),false);
   await page.mouse.move(1,1);
   await first.locator('.account-info').focus();
   assert.equal(await tip.isVisible(),true,'帳號資訊保留鍵盤焦點提示');
   await first.locator('.reset-account').focus();
   assert.equal(await tip.isVisible(),false,'按鈕焦點不會觸發帳號提示');
   await gap(first);
   await page.evaluate(()=>loadAccounts());
   assert.equal(await page.locator('button[title]').count(),0,'列表重新產生後仍不可有原生提示');
   await gap(first);
   assert.equal(await tip.isVisible(),false,'重新整理後仍限制觸發區域');
  }
  assert.deepEqual(errors,[],'無 JavaScript 執行錯誤');
 }finally{await browser.close();}
});
