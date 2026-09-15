const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require('playwright');
const html=fs.readFileSync(path.join(__dirname,'../web/index.html'),'utf8').replace('/* STYLE_PLACEHOLDER */',fs.readFileSync(path.join(__dirname,'../web/style.css'),'utf8'));
test('data URL: imported ID remains data; reset/login work without localStorage; update bridge and progress work',async()=>{
 const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'msedge',headless:true});
 try{
  const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>route.abort());
  await page.addInitScript(()=>{
   window.getAccounts=async()=>[{id:'sample',email:'sample@example.com'}];
   window.startLogin=async()=>({account:{}});
   window.resetCalls=0;
   window.resetAccountAction=async(action)=>action==='consume'?(window.resetCalls++,{code:'reset'}):{credits:[{id:'fixture-credit',expires_at:'2027-01-01T00:00:00Z'}],available_count:1};
   window.updateState={phase:'current',message:'目前已是最新版本。'};
   window.checks=0;
   window.checkForUpdate=async()=>{window.checks++;window.updateState={phase:'available',message:'新版',version:'1.26.0916-build-0000'}};
   window.getUpdateStatus=async()=>window.updateState;
   window.installUpdate=async()=>{window.updateState={phase:'downloading',downloaded:50,total:100}};
  });
  await page.goto('data:text/html;base64,'+Buffer.from(html).toString('base64'));
  await page.waitForFunction(()=>document.querySelector('.account'));
  assert.equal(await page.evaluate(()=>{try{localStorage.getItem('probe');return false}catch{return true}}),true);
  const malicious="');window.injected=true;//";
  const injected=await page.evaluate(id=>{
   window.injected=false;window.capturedID=null;openSettings=id=>window.capturedID=id;
   render([{id,email:'fixture@example.com'}]);document.querySelector('.account-settings').click();
   return {injected:window.injected,id:window.capturedID};
  },malicious);
  assert.deepEqual(injected,{injected:false,id:malicious});
  await page.evaluate(()=>loadAccounts());
  await page.locator('#email').fill('sample@example.com');
  await page.locator('#loginButton').click();
  await page.waitForFunction(()=>document.querySelector('#message').textContent.includes('登入成功'));
  await page.evaluate(()=>openReset('sample'));
  await page.evaluate(()=>consumeReset(0));
  assert.equal(await page.evaluate(()=>window.resetCalls),1);
  await page.evaluate(()=>closeReset());
  await page.evaluate(()=>openAbout());
  await page.locator('#updateButton').click();
  await page.waitForFunction(()=>document.querySelector('#updateButton').textContent==='下載並安裝');
  assert.equal(await page.evaluate(()=>window.checks),1,'後端更新接口只呼叫一次');
  await page.locator('#updateButton').click();
  await page.waitForFunction(()=>document.querySelector('#updateStatus').textContent.includes('50%'));
  assert.equal(await page.locator('#updateProgress span').evaluate(el=>el.style.width),'50%');
  await page.evaluate(()=>{window.updateState={phase:'error',message:'下載失敗'};return refreshUpdateStatus()});
  assert.equal(await page.locator('#updateButton').isDisabled(),false,'錯誤後可以重試');
  assert.deepEqual(errors,[]);
 }finally{await browser.close()}
});
