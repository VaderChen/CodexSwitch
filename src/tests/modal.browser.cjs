const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require('playwright');
const html=fs.readFileSync(path.join(__dirname,'../web/index.html'),'utf8').replace('/* STYLE_PLACEHOLDER */',fs.readFileSync(path.join(__dirname,'../web/style.css'),'utf8'));
async function harness(t,options={}){
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'msedge',headless:true});
 t.after(()=>browser.close());
 const page=await browser.newPage(),errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 t.after(()=>assert.deepEqual(errors,[]));
 await page.route('**/*',route=>route.abort());
 await page.addInitScript(({disableInert,startupAvailable})=>{
  if(disableInert)Object.defineProperty(HTMLElement.prototype,'inert',{get(){return false},set(){}});
  window.accounts=[{id:'one',email:'one@example.invalid'},{id:'two',email:'two@example.invalid'}];
  window.getAccounts=async()=>window.accounts;
  window.deleteCalls=0;window.applyCalls=0;
  window.deleteAccount=async id=>{window.deleteCalls++;window.accounts=window.accounts.filter(a=>a.id!==id);return {ok:true}};
  window.useAccount=async()=>{window.applyCalls++;return {email:'two@example.invalid'}};
  window.getAccountDefaults=async()=>({codex_home:'/fixture/home',user_data_dir:'/fixture/data'});
  window.resetAccountAction=async()=>({credits:[],available_count:0});
  window.updateState={phase:'current'};window.checks=0;window.installs=0;
  window.checkForUpdate=async()=>{window.checks++;if(startupAvailable)window.updateState={phase:'available',version:'fixture'}};
  window.getUpdateStatus=async()=>window.updateState;
  window.installUpdate=async()=>{window.installs++;window.updateState={phase:'downloading',downloaded:50,total:100}};
 },options);
 await page.goto('data:text/html;base64,'+Buffer.from(html).toString('base64'));
 await page.waitForFunction(()=>document.querySelectorAll('.account').length===2);
 return page;
}
for(const disableInert of [false,true])test(`移除對話框隔離焦點與套用流程（inert ${disableInert?'fallback':'native'}）`,async t=>{
 const page=await harness(t,{disableInert});
 await page.locator('.account').first().locator('.delete').click();
 assert.equal(await page.evaluate(()=>document.activeElement.id),'dialogCancel');
 for(let i=0;i<8;i++){
  await page.keyboard.press(i%2?'Shift+Tab':'Tab');
  assert.equal(await page.evaluate(()=>document.querySelector('#dialog').contains(document.activeElement)),true);
 }
 await page.evaluate(()=>handleApplyAccount('two'));
 assert.equal(await page.locator('#dialogTitle').textContent(),'移除帳號');
 assert.equal(await page.evaluate(()=>window.applyCalls),0);
 await page.keyboard.press('Escape');
 assert.equal(await page.locator('#dialog').isVisible(),false);
 assert.equal(await page.evaluate(()=>document.activeElement.classList.contains('delete')),true);
 assert.equal(await page.evaluate(()=>removeBusy),false);
 await page.locator('.account').first().locator('.delete').click();
 await page.locator('#dialogOK').click();
 await page.waitForFunction(()=>window.deleteCalls===1 && document.querySelectorAll('.account').length===1);
 assert.equal(await page.locator('#dialog').isVisible(),false);
});
test('設定、重置與關於使用相同的焦點與 Escape 管理',async t=>{
 const page=await harness(t);
 for(const [button,id] of [['.account-settings','settingsDialog'],['.reset-account','resetDialog'],['.about-link','aboutDialog']]){
  await page.locator(button).first().click();
  await page.waitForFunction(id=>document.querySelector('#'+id).contains(document.activeElement),id);
  for(let i=0;i<6;i++){
   await page.keyboard.press('Tab');
   assert.equal(await page.evaluate(id=>document.querySelector('#'+id).contains(document.activeElement),id),true);
  }
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#'+id).isVisible(),false);
 }
});
test('啟動發現新版後，第一次按下更新直接安裝',async t=>{
 const page=await harness(t,{startupAvailable:true});
 await page.waitForFunction(()=>!document.querySelector('#updateNotice').hidden);
 await page.locator('.about-link').click();
 await page.waitForFunction(()=>document.querySelector('#updateButton').dataset.install==='true');
 await page.locator('#updateButton').click();
 await page.waitForFunction(()=>document.querySelector('#updateStatus').textContent.includes('50%'));
 assert.equal(await page.evaluate(()=>window.checks),1);
 assert.equal(await page.evaluate(()=>window.installs),1);
});

test('設定載入時保留目前帳號，關閉後忽略延遲回應',async t=>{
 const page=await harness(t);
 await page.evaluate(()=>{
  window.defaultsRequests=[];
  window.getAccountDefaults=()=>new Promise(resolve=>window.defaultsRequests.push(resolve));
 });
 await page.locator('.account').first().locator('.account-settings').click();
 assert.equal(await page.locator('#settingsDialog').isVisible(),true);
 assert.equal(await page.locator('#settingsEmail').textContent(),'one@example.invalid');
 assert.equal(await page.locator('#settingsSave').isDisabled(),true);
 await page.evaluate(()=>{void openSettings('two')});
 assert.equal(await page.evaluate(()=>window.defaultsRequests.length),1);
 await page.keyboard.press('Escape');
 assert.equal(await page.locator('#settingsDialog').isVisible(),false);
 await page.locator('.account').nth(1).locator('.account-settings').click();
 await page.evaluate(()=>window.defaultsRequests[0]({codex_home:'/fixture/old',user_data_dir:'/fixture/old-data'}));
 assert.equal(await page.locator('#settingsEmail').textContent(),'two@example.invalid');
 assert.equal(await page.locator('#settingsSave').isDisabled(),true);
 await page.evaluate(()=>window.defaultsRequests[1]({codex_home:'/fixture/new',user_data_dir:'/fixture/new-data'}));
 await page.waitForFunction(()=>!document.querySelector('#settingsSave').disabled);
 assert.equal(await page.locator('#settingsEmail').textContent(),'two@example.invalid');
 assert.equal(await page.locator('#settingsHome').inputValue(),'/fixture/new');
});

test('帳號重新整理忽略較舊的回應與錯誤',async t=>{
 const page=await harness(t);
 await page.evaluate(()=>{
  window.accountRequests=[];
  window.getAccounts=()=>new Promise((resolve,reject)=>window.accountRequests.push({resolve,reject}));
  void loadAccounts();void loadAccounts();
  window.accountRequests[1].resolve([window.accounts[0]]);
 });
 await page.waitForFunction(()=>document.querySelectorAll('.account').length===1);
 await page.evaluate(()=>window.accountRequests[0].resolve(window.accounts));
 assert.equal(await page.locator('.account').count(),1);
 await page.evaluate(()=>{
  void loadAccounts();void loadAccounts();
  window.accountRequests[3].resolve([window.accounts[0]]);
 });
 await page.evaluate(()=>window.accountRequests[2].reject(Error('已過期的讀取失敗')));
 assert.equal(await page.locator('#message').textContent(),'');
});

test('背景重新整理帳號後關閉設定，焦點回到對應帳號按鈕',async t=>{
 const page=await harness(t);
 await page.locator('.account').first().locator('.account-settings').click();
 await page.waitForFunction(()=>document.querySelector('#settingsHome')===document.activeElement);
 await page.evaluate(()=>loadAccounts());
 await page.keyboard.press('Escape');
 assert.equal(await page.evaluate(()=>document.activeElement.dataset.action),'settings');
 assert.equal(await page.evaluate(()=>document.activeElement.closest('.account')?.dataset.accountId),'one');
});

test('設定儲存及刷新完成前保持對話框，完成後還原焦點',async t=>{
 const page=await harness(t);
 await page.evaluate(()=>{
  window.saveCalls=[];
  window.saveAccountSettings=(...args)=>{
   window.saveCalls.push(args);
   return new Promise(resolve=>window.completeSave=resolve);
  };
  window.getAccounts=()=>new Promise(resolve=>window.completeReload=resolve);
 });
 await page.locator('.account').first().locator('.account-settings').click();
 await page.waitForFunction(()=>!document.querySelector('#settingsSave').disabled);
 await page.locator('#settingsHome').fill('/fixture/changed');
 await page.locator('#settingsSave').click();
 await page.keyboard.press('Escape');
 assert.equal(await page.locator('#settingsDialog').isVisible(),true);
 assert.deepEqual(await page.evaluate(()=>window.saveCalls),[['one','/fixture/changed','/fixture/data']]);
 await page.evaluate(()=>window.completeSave({ok:true}));
 await page.waitForFunction(()=>typeof window.completeReload==='function');
 await page.keyboard.press('Escape');
 assert.equal(await page.locator('#settingsDialog').isVisible(),true);
 await page.evaluate(()=>window.completeReload(window.accounts));
 await page.waitForFunction(()=>document.querySelector('#settingsDialog').hidden);
 assert.equal(await page.evaluate(()=>document.activeElement.dataset.action),'settings');
 assert.equal(await page.evaluate(()=>document.activeElement.closest('.account')?.dataset.accountId),'one');
});
