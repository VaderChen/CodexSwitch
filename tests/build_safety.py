"""Build an isolated fixture to verify failure preserves the previous release."""
from pathlib import Path
import hashlib, os, shutil, subprocess, tempfile
source=Path(__file__).resolve().parents[1]/'build.command'

def snapshot(path):
 return {str(p.relative_to(path)):hashlib.sha256(p.read_bytes()).hexdigest() for p in path.rglob('*') if p.is_file()}

with tempfile.TemporaryDirectory(prefix='codexswitch-build-safety-') as folder:
 root=Path(folder)
 shutil.copy2(source,root/'build.command')
 (root/'src/assets').mkdir(parents=True)
 (root/'src/assets/CodexSwitch.icns').write_bytes(b'fixture icon')
 (root/'src/go.mod').write_text('module fixture\n\ngo 1.27.1\n')
 (root/'src/main.go').write_text('package main\nfunc main() { syntax error }\n')
 (root/'dist').mkdir()
 (root/'dist/previous.app').write_bytes(b'last good app')
 (root/'dist/previous.dmg').write_bytes(b'last good package')
 before=snapshot(root/'dist')
 result=subprocess.run([str(root/'build.command')],cwd=root,capture_output=True,text=True)
 assert result.returncode!=0, result.stdout+result.stderr
 assert snapshot(root/'dist')==before, 'FAILED: compilation failure deleted/replaced previous dist'
 assert not list(root.glob('.codexswitch-build.*')), 'staging directory leaked'
 assert not (root/'dist.bak').exists(), 'failed compile moved dist to backup'
 print('PASS: compile failure preserves existing dist and cleans staging')
 shutil.copy2(source.with_name('run.command'), root/'run.command')
 (root/'bin').mkdir()
 killer=root/'bin/pkill'
 killer.write_text('#!/bin/sh\nprintf called > "$CODEX_SWITCH_TEST_KILL_LOG"\n')
 killer.chmod(0o700)
 env=os.environ.copy()
 env['PATH']=str(root/'bin')+':'+env['PATH']
 env['CODEX_SWITCH_TEST_KILL_LOG']=str(root/'killed')
 result=subprocess.run([str(root/'run.command')],cwd=root,env=env,capture_output=True,text=True)
 assert result.returncode!=0
 assert not (root/'killed').exists(), 'failed rebuild terminated the running app'
 assert snapshot(root/'dist')==before
 print('PASS: run.command leaves running app alone when rebuild fails')
 (root/'src/main.go').write_text('package main\nfunc main() {}\n')
 result=subprocess.run([str(root/'build.command')],cwd=root,capture_output=True,text=True)
 assert result.returncode==0,result.stdout+result.stderr
 assert (root/'dist/CodexSwitch.app/Contents/MacOS/CodexSwitch').is_file()
 assert not (root/'dist/previous.dmg').exists()
 assert not (root/'dist.bak').exists()
 assert not list(root.glob('.codexswitch-build.*'))
 print('PASS: successful build replaces dist and removes backup/staging')
 (root/'dist.bak').mkdir()
 (root/'dist.bak/recovery').write_text('preserve me')
 before=snapshot(root/'dist')
 result=subprocess.run([str(root/'build.command')],cwd=root,capture_output=True,text=True)
 assert result.returncode!=0
 assert snapshot(root/'dist')==before
 assert (root/'dist.bak/recovery').read_text()=='preserve me'
 print('PASS: preexisting backup blocks build without modification')
