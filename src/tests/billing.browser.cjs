const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require('playwright');
const html=fs.readFileSync(path.join(__dirname,'../web/index.html'),'utf8').replace('/* STYLE_PLACEHOLDER */',fs.readFileSync(path.join(__dirname,'../web/style.css'),'utf8'));
const invoice=(id,overrides={})=>({type:'invoice',id,created_at:'2026-09-20T12:00:00Z',amount:2000,currency:'usd',status:'paid',product:{type:'ChatGPT',plan:'Plus'},download_id:'download-'+id,...overrides});
async function harness(t,options={}){
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'msedge',headless:true});
 t.after(()=>browser.close());
 const page=await browser.newPage({viewport:{width:options.width||760,height:780}}),errors=[];
 page.on('pageerror',e=>errors.push(e.message));t.after(()=>assert.deepEqual(errors,[]));
 await page.route('**/*',route=>route.abort());
 await page.addInitScript(({disableInert,fastTimeout})=>{
  if(disableInert)Object.defineProperty(HTMLElement.prototype,'inert',{get(){return false},set(){}});
  if(fastTimeout){const original=window.setTimeout;window.setTimeout=(fn,ms,...args)=>original(fn,ms===30000?50:ms,...args);}
  window.accounts=[{id:'one',email:'billing.long.account@example.invalid',is_current:true},{id:'two',email:'two@example.invalid'}];
  window.getAccounts=async()=>window.accounts;
  window.queryCalls=[];window.invoiceCalls=[];window.cancelCalls=[];
  window.queryBilling=async(...args)=>{window.queryCalls.push(args);return {pending:true};};
  window.openBillingInvoice=async(...args)=>{window.invoiceCalls.push(args);return {pending:true};};
  window.cancelBilling=async id=>{window.cancelCalls.push(id);return {ok:true};};
 },options);
 await page.goto('data:text/html;base64,'+Buffer.from(html).toString('base64'));
 await page.waitForFunction(()=>document.querySelectorAll('.account').length===2);
 return page;
}
async function open(page,index=0){await page.locator('[data-action="billing"]').nth(index).click();}
async function query(page){await page.locator('#billingQuery').click();}
async function reply(page,payload,index){
 await page.evaluate(({payload,index})=>{
  const call=window.queryCalls[index??window.queryCalls.length-1];
  window.receiveBillingResult(call[2],payload);
 },{payload,index});
 await page.waitForFunction(()=>!document.querySelector('#billingQuery').disabled);
}
for(const disableInert of [false,true])test(`帳單只在按下查詢後存取；關閉及切換帳號忽略舊結果（inert ${disableInert?'fallback':'native'}）`,async t=>{
 const page=await harness(t,{disableInert});
 assert.equal(await page.evaluate(()=>window.queryCalls.length),0);
 await open(page);
 assert.equal(await page.evaluate(()=>window.queryCalls.length),0);
 assert.equal(await page.evaluate(()=>document.activeElement.id),'billingQuery');
 for(let i=0;i<8;i++){
  await page.keyboard.press(i%2?'Shift+Tab':'Tab');
  assert.equal(await page.evaluate(()=>document.querySelector('#billingDialog').contains(document.activeElement)),true);
 }
 await query(page);
 assert.equal(await page.locator('#billingSpinner').isVisible(),true);
 await page.evaluate(()=>{void handleQueryBilling();void handleQueryBilling(true);});
 assert.equal(await page.evaluate(()=>window.queryCalls.length),1);
 await page.keyboard.press('Escape');
 assert.equal(await page.locator('#billingDialog').isVisible(),false);
 assert.equal(await page.evaluate(()=>document.activeElement.dataset.action),'billing');
 assert.deepEqual(await page.evaluate(()=>window.cancelCalls),[await page.evaluate(()=>window.queryCalls[0][2])]);
 await open(page,1);
 assert.equal(await page.evaluate(()=>document.activeElement.id),'billingQuery');
 await query(page);
 await page.evaluate(payload=>window.receiveBillingResult(window.queryCalls[0][2],payload),{transactions:[invoice('old')],next_cursor:'old-cursor'});
 assert.equal(await page.locator('#billingRows tr').count(),0);
 assert.equal(await page.locator('#billingQuery').isDisabled(),true);
 await reply(page,{transactions:[invoice('new')],next_cursor:''},1);
 assert.equal(await page.locator('#billingEmail').textContent(),'two@example.invalid');
 assert.equal(await page.locator('#billingRows tr').count(),1);
 assert.deepEqual(await page.evaluate(()=>window.queryCalls.map(call=>call.slice(0,2))),[['one',''],['two','']]);
});

test('帳單分頁去重、防循環游標；重新查詢清除舊頁並呈現空白狀態',async t=>{
 const page=await harness(t);await open(page);await query(page);
 await reply(page,{transactions:[invoice('usd'),invoice('jpy',{amount:3000,currency:'jpy',created_at:'2026-08-20T12:00:00Z'})],next_cursor:'page-a'});
 assert.match(await page.locator('#billingRows tr').nth(0).locator('td').nth(2).textContent(),/USD\s*20\.00/);
 assert.match(await page.locator('#billingRows tr').nth(1).locator('td').nth(2).textContent(),/JPY\s*3,000/);
 await page.locator('#billingMore').click();
 await reply(page,{transactions:[invoice('usd'),invoice('third',{created_at:'2026-07-20T12:00:00Z',amount:12345,currency:'kwd'})],next_cursor:'page-b'});
 assert.equal(await page.locator('#billingRows tr').count(),3);
 assert.match(await page.locator('#billingRows tr').nth(2).locator('td').nth(2).textContent(),/KWD\s*12\.345/);
 await page.locator('#billingMore').click();
 await reply(page,{transactions:[invoice('fourth',{created_at:'2026-06-20T12:00:00Z'})],next_cursor:'page-a'});
 assert.equal(await page.locator('#billingRows tr').count(),4);
 assert.equal(await page.locator('#billingMore').isVisible(),false);
 await query(page);assert.equal(await page.locator('#billingRows tr').count(),0);
 await reply(page,{transactions:[],next_cursor:''});
 assert.equal(await page.locator('#billingMessage').textContent(),'目前沒有交易紀錄。');
 assert.equal(await page.locator('#billingTable').isVisible(),false);
 assert.deepEqual(await page.evaluate(()=>window.queryCalls.map(call=>call[1])),['','page-a','page-b','']);
 assert.equal(await page.evaluate(()=>new Set(window.queryCalls.map(call=>call[2])).size),4);
});

test('查詢失敗與畸形資料可重試；較早紀錄失敗保留既有資料',async t=>{
 const page=await harness(t);await open(page);
 for(const payload of [null,{transactions:null},{transactions:[null]},{transactions:[{id:'missing-type'}]},{transactions:[],next_cursor:1},{error:'請重新登入'}]){
  await query(page);await reply(page,payload);
  assert.equal(await page.locator('#billingMessage').evaluate(el=>el.classList.contains('error')),true);
  assert.equal(await page.locator('#billingRows tr').count(),0);
  assert.equal(await page.locator('#billingQuery').isDisabled(),false);
 }
 await query(page);await reply(page,{transactions:[invoice('kept')],next_cursor:'older'});
 await page.locator('#billingMore').click();await reply(page,{error:'暫時無法連線'});
 assert.equal(await page.locator('#billingRows tr').count(),1);
 assert.equal(await page.locator('#billingMore').isDisabled(),false);
 await page.locator('#billingMore').click();await reply(page,{transactions:[invoice('next')],next_cursor:''});
 assert.equal(await page.locator('#billingRows tr').count(),2);
});

test('bridge 立即失敗、遺失及 callback 早於 pending 回傳均能結束',async t=>{
 const page=await harness(t);await open(page);
 await page.evaluate(()=>window.queryBilling=async()=>({error:'立即失敗'}));
 await query(page);await page.waitForFunction(()=>!document.querySelector('#billingQuery').disabled);
 assert.equal(await page.locator('#billingMessage').textContent(),'立即失敗');
 await page.evaluate(()=>delete window.queryBilling);
 await query(page);await page.waitForFunction(()=>!document.querySelector('#billingQuery').disabled);
 assert.match(await page.locator('#billingMessage').textContent(),/尚未就緒/);
 await page.evaluate(()=>window.queryBilling=(_id,_cursor,requestID)=>{
  window.receiveBillingResult(requestID,{transactions:[],next_cursor:''});
  return new Promise(()=>{});
 });
 await query(page);await page.waitForFunction(()=>!document.querySelector('#billingQuery').disabled);
 assert.equal(await page.locator('#billingMessage').textContent(),'目前沒有交易紀錄。');
 assert.equal(await page.evaluate(()=>billingWaiters.size),0);
});

test('帳單下載只傳帳號與 opaque ID；重複點擊、下載錯誤與關閉均正確處理',async t=>{
 const page=await harness(t);await open(page);await query(page);
 await reply(page,{transactions:[invoice('one'),invoice('none',{download_id:undefined})],next_cursor:''});
 assert.equal(await page.locator('#billingRows tr').nth(1).locator('button').isDisabled(),true);
 const button=page.locator('#billingRows tr').first().locator('button');
 await button.click();await page.evaluate(()=>{void downloadBillingInvoice('download-one')});
 assert.equal(await page.evaluate(()=>window.invoiceCalls.length),1);
 assert.equal(await button.isDisabled(),true);
 assert.deepEqual(await page.evaluate(()=>window.invoiceCalls[0].slice(0,2)),['one','download-one']);
 assert.notEqual(await page.evaluate(()=>window.invoiceCalls[0][2]),await page.evaluate(()=>window.queryCalls[0][2]));
 await page.evaluate(()=>window.receiveBillingResult(window.invoiceCalls[0][2],{error:'帳單連結已過期，請重新查詢'}));
 await page.waitForFunction(()=>!document.querySelector('#billingRows button').disabled);
 assert.match(await page.locator('#billingMessage').textContent(),/已過期/);
 await button.click();
 await page.evaluate(()=>window.receiveBillingResult(window.invoiceCalls[1][2],{opened:true}));
 await page.waitForFunction(()=>!document.querySelector('#billingRows button').disabled);
 assert.match(await page.locator('#billingMessage').textContent(),/已開啟 Stripe 帳單頁.*PDF 或收據/);
 assert.equal(await page.locator('#billingDialog a').count(),0);
 await button.click();await page.keyboard.press('Escape');
 await open(page,1);
 await page.evaluate(()=>window.receiveBillingResult(window.invoiceCalls[2][2],{opened:true}));
 assert.match(await page.locator('#billingMessage').textContent(),/按下「查詢帳單」/);
 assert.equal(await page.locator('#billingEmail').textContent(),'two@example.invalid');
});

test('查詢與下載逾時會取消 pending，不會永久停用控制項',async t=>{
 const page=await harness(t,{fastTimeout:true});await open(page);await query(page);
 await page.waitForFunction(()=>document.querySelector('#billingMessage').textContent.includes('逾時'));
 assert.equal(await page.locator('#billingQuery').isDisabled(),false);
 assert.equal(await page.evaluate(()=>billingWaiters.size),0);
 assert.equal(await page.evaluate(()=>window.cancelCalls.length),1);
 await page.evaluate(payload=>window.queryBilling=async(...args)=>{
  window.queryCalls.push(args);
  window.receiveBillingResult(args[2],payload);
  return {pending:true};
 },{transactions:[invoice('timed')],next_cursor:''});
 await query(page);
 await page.locator('#billingRows button').click();
 await page.waitForFunction(()=>document.querySelector('#billingMessage').textContent.includes('逾時'));
 assert.equal(await page.locator('#billingRows button').isDisabled(),false);
 assert.equal(await page.evaluate(()=>billingWaiters.size),0);
 assert.equal(await page.evaluate(()=>window.cancelCalls.length),2);
});

for(const width of [520,760])test(`帳單安全文字呈現與 ${width}px 視窗無水平溢位`,async t=>{
 const page=await harness(t,{width});await open(page);await query(page);
 const malicious='<img src=x onerror="window.billingInjected=true">';
 await reply(page,{transactions:[invoice(malicious,{created_at:'invalid',amount:'2000',currency:'<x>',status:'__proto__',product:{type:malicious,plan:'非常長的產品名稱'.repeat(12)},download_id:'opaque'+malicious}),invoice('normal',{product:{type:'ChatGPT',plan:'Plus'}})],next_cursor:'older'});
 assert.equal(await page.locator('#billingRows img').count(),0);
 assert.equal(await page.evaluate(()=>window.billingInjected),undefined);
 assert.match(await page.locator('#billingRows').textContent(),/<img src=x/);
 assert.match(await page.locator('#billingRows').textContent(),/日期未提供|金額未提供/);
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
 assert.equal(await page.locator('.billing-dialog').evaluate(el=>el.scrollWidth<=el.clientWidth),true);
 assert.equal(await page.locator('#billingList').evaluate(el=>el.scrollWidth<=el.clientWidth),true);
 for(let i=0;i<10;i++){
  await page.keyboard.press('Tab');
  assert.equal(await page.evaluate(()=>document.querySelector('#billingDialog').contains(document.activeElement)),true);
 }
 await page.keyboard.press('Escape');
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
 if(process.env.BILLING_SCREENSHOT_DIR){
  await open(page);await query(page);
  await reply(page,{transactions:[invoice('recent'),invoice('jpy',{created_at:'2026-08-20T12:00:00Z',amount:3000,currency:'jpy',product:{type:'ChatGPT',plan:'Plus'}}),invoice('previous',{created_at:'2026-07-20T12:00:00Z',status:'refunded',amount:-2000})],next_cursor:'older'});
  await page.screenshot({path:path.join(process.env.BILLING_SCREENSHOT_DIR,`billing-${width}.png`)});
 }
});
