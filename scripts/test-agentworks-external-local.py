#!/usr/bin/env python3
"""Exercise built CLI + stdio MCP against isolated real AgentWorks services.
Uses only design inputs from Workflow/testing; never runs the retained workflow.
Writes an evidence receipt and stops only the processes started by this script.
"""
import hashlib
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import tempfile
import time
import urllib.request
import urllib.error
import selectors
import wave

REPO = Path(__file__).resolve().parents[1]
SOURCE = REPO / 'workspace-docs/Workflow/testing'
INPUTS = ('workflow.json', 'planning/plan.json', 'planning/step_config.json')

def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0)); return s.getsockname()[1]
def http(url, data=None, token=None, expected=200):
    headers = {'Content-Type':'application/json'}
    if token: headers['Authorization']='Bearer '+token
    req=urllib.request.Request(url, data=None if data is None else json.dumps(data).encode(), headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=45) as r: status,raw=r.status,r.read()
    except urllib.error.HTTPError as e: status,raw=e.code,e.read()
    if status != expected: raise RuntimeError(f'HTTP {status}, expected {expected}: {raw[:1500].decode(errors="replace")}')
    return json.loads(raw) if raw else {}

class Probe:
    def __init__(self):
        parent=REPO/'.local/workflow-tests';parent.mkdir(parents=True,exist_ok=True)
        self.root=Path(tempfile.mkdtemp(prefix='external-api-',dir=parent));self.root.chmod(0o700)
        self.docs=self.root/'workspace-docs';self.fixture=self.docs/'Workflow/testing'
        self.before={p:digest(SOURCE/p) for p in INPUTS}
        self.processes=[];self.logs=[];self.results=[]
        self.server=f'http://127.0.0.1:{port()}';self.workspace=f'http://127.0.0.1:{port()}'
        self.password=secrets.token_urlsafe(24)
        self.env={k:os.environ[k] for k in ('PATH','HOME','TMPDIR','SHELL','LANG') if k in os.environ}
        self.env.update({'AUTH_SECRET':secrets.token_hex(32),'AUTH_USERS':'external-owner:'+self.password,
         'ADMIN_USERS':'external-owner','MULTI_USER_MODE':'true','AUTH_PROVIDERS':'simple',
         'WORKSPACE_API_TOKEN':secrets.token_hex(32),'WORKSPACE_API_URL':self.workspace,
         'WORKSPACE_DOCS_PATH':str(self.docs),'RUNLOOP_DOCS_DIR':str(self.docs),
         'AGENTWORKS_STATE_ROOT':str(self.root/'state'),'AGENTWORKS_INSTANCE_ID':self.root.name,
         'AGENTWORKS_LOG_DIR':str(self.root/'logs'),'MCP_CACHE_DIR':str(self.root/'cache'),
         'MCP_AGENT_SERVER_URL':self.server,'AGENT_SERVER_URL':self.server,
         'MCP_API_URL':self.server,'CLI_UPDATE_ENABLED':'false',
         'AGENTWORKS_SKIP_GLOBAL_BROWSER_CLEANUP':'true','AGENTWORKS_SKIP_GLOBAL_DEPENDENCY_UPDATES':'true',
         'AGENTWORKS_STRICT_PROCESS_OWNERSHIP':'true','AGENTWORKS_ISOLATE_WORKFLOW_CLI':'true',
         'AGENTWORKS_BROWSER_SESSION_PREFIX':self.root.name,'TMUX_TMPDIR':str(self.root/'tmux'),
         'LOG_LEVEL':'error','NO_COLOR':'1'})
        for d in ('bin','logs','cache','state','tmux','runtime'):(self.root/d).mkdir()
        (self.root/'runtime/.env').write_text('');(self.root/'runtime/config.yaml').write_text('{}\n')
        (self.root/'runtime/mcp.json').write_text('{"mcpServers":{}}\n')
        self.cli=self.root/'bin/agentworks';self.config=self.root/'client.json'
        self.token=None
    def record(self,name,**details):
        self.results.append({'name':name,'passed':True,**details});print('PASS:',name,flush=True)
    def build(self):
        for name,cwd,target in [('agent-server','agent_go','.'),('workspace-server','workspace','.'),('agentworks','agent_go','./cmd/agentworks')]:
            print('Building',name,flush=True)
            with (self.root/'logs'/f'build-{name}.log').open('w') as log:
                subprocess.run(['go','build','-o',str(self.root/'bin'/name),target],cwd=REPO/cwd,env=self.env,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=300)
    def prepare(self):
        self.fixture.mkdir(parents=True)
        for rel in INPUTS[1:]:
            (self.fixture/rel).parent.mkdir(parents=True,exist_ok=True)
            (self.fixture/rel).write_bytes((SOURCE/rel).read_bytes())
        original=json.loads((SOURCE/'workflow.json').read_text())
        manifest={'id':'external-local-testing','label':'testing (isolated MCP/CLI test)',
          'schema_version':original.get('schema_version',1),'version':original.get('version',''),
          'schedules':[],'capabilities':{'selected_servers':[],'selected_skills':[],
          'selected_tools':[],'selected_secrets':[],'selected_global_secret_names':[],'browser_mode':'none'},
          'pulse':{'enabled':False},'backup':{'enabled':False},'publish':{'enabled':False}}
        (self.fixture/'workflow.json').write_text(json.dumps(manifest,indent=2))
        (self.fixture/'DO-NOT-EXECUTE.txt').write_text('Copied plan is design evidence only. Do not execute retained source workflow steps.\n')
        (self.fixture/'db/assets').mkdir(parents=True)
        with wave.open(str(self.fixture/'db/assets/local smoke.wav'),'wb') as asset:
            asset.setnchannels(1);asset.setsampwidth(2);asset.setframerate(16000);asset.writeframes(bytes(3<<20))
        (self.fixture/'docs').mkdir();(self.fixture/'docs/readme.md').write_text('AgentWorks external local smoke marker.\n')
        # Saved run artifacts are synthetic, not claims about an executed run.
        run=self.fixture/'runs/iteration-smoke/smoke';(run/'logs').mkdir(parents=True)
        (run/'logs/smoke.log').write_text('Synthetic run log for file inspection; no workflow executed.\n')
    def start(self,name,cmd,health):
        log=(self.root/'logs'/f'{name}.log').open('w');self.logs.append(log)
        proc=subprocess.Popen(cmd,cwd=self.root/'runtime',env=self.env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
        self.processes.append(proc)
        deadline=time.monotonic()+60
        while time.monotonic()<deadline:
            if proc.poll() is not None: raise RuntimeError(f'{name} exited {proc.returncode}; see {self.root}/logs/{name}.log')
            try: http(health);return
            except (OSError,RuntimeError):time.sleep(.3)
        raise RuntimeError(name+' did not become healthy')
    def launch(self):
        self.start('workspace',[str(self.root/'bin/workspace-server'),'server','--host','127.0.0.1','--port',self.workspace.rsplit(':',1)[1],'--docs-dir',str(self.docs)],self.workspace+'/health')
        self.start('agent',[str(self.root/'bin/agent-server'),'--config',str(self.root/'runtime/config.yaml'),'--trace-provider','noop','server','--host','127.0.0.1','--port',self.server.rsplit(':',1)[1],'--provider','codex-cli','--model','gpt-5.4','--mcp-config',str(self.root/'runtime/mcp.json')],self.server+'/api/health')
        self.record('isolated real services started',agent_url=self.server,workspace_url=self.workspace)
    def command(self,*args,input=None,expected=0):
        p=subprocess.run([str(self.cli),'--config',str(self.config),'--server',self.server,'--json',*args],cwd=self.root/'runtime',env=self.env,input=input,text=True,capture_output=True,timeout=60)
        if p.returncode!=expected: raise RuntimeError(f'CLI {args[:3]} exit {p.returncode}, expected {expected}: {p.stderr[:2000]}')
        raw=p.stdout if expected==0 else p.stderr
        return json.loads(raw) if raw.strip() else {}
    def call(self,name,args,expected=0): return self.command('tools','call',name,'--input','-',input=json.dumps(args),expected=expected)
    def test_cli(self):
        app=http(self.server+'/api/auth/login',{'username':'external-owner','password':self.password})
        self.app_token=app['token']
        issued=http(self.server+'/api/auth/access-tokens',{'name':'Local CLI and MCP','scopes':['workflows:read','files:read','files:write','plan:write','builder:chat'],'all_workflows':True,'expires_in_days':7},self.app_token,expected=201)
        self.pat_id=issued['access_token']['id']
        self.command('login','--token-stdin',input=issued['token']+'\n')
        self.token=json.loads(self.config.read_text())['token']
        assert self.config.stat().st_mode&0o077==0
        self.record('App-generated PAT login and private CLI credentials')
        workflows=self.command('workflows','list');assert len(workflows['workflows'])==1
        self.wid=workflows['workflows'][0]['manifest']['id']
        tools=self.command('tools','list');assert any(t['name']=='update_message_sequence_step' for t in tools['tools'])
        self.record('workflow discovery and tool schemas',workflow_id=self.wid,tool_count=len(tools['tools']))
        args={'workflow_id':self.wid,'path':'docs/readme.md'}
        initial=self.call('read_file',args);assert 'smoke marker' in initial['content']
        self.call('write_file',{**args,'content':'AgentWorks CLI UPDATED marker.\n','expected_revision':initial['revision']})
        self.call('write_file',{**args,'content':'stale','expected_revision':initial['revision']},expected=4)
        found=self.call('search_files',{'workflow_id':self.wid,'path':'docs','query':'UPDATED'});assert found['entries']
        current=self.call('read_file',args)
        self.call('patch_file',{**args,'expected_revision':current['revision'],'diff':'--- a/readme.md\n+++ b/readme.md\n@@ -1 +1 @@\n-AgentWorks CLI UPDATED marker.\n+AgentWorks CLI PATCHED marker.\n'})
        assert 'PATCHED' in self.call('read_file',args)['content']
        self.record('file read/write/patch/search and stale-write rejection')
        link=self.command('files','link','--workflow',self.wid,'--path','db/assets/local smoke.wav')
        assert link['size']>2<<20 and '/file?path=' in link['preview_url'] and 'token=' not in link['preview_url']
        downloaded=self.root/'downloaded.wav'
        self.command('files','download','--workflow',self.wid,'--path','db/assets/local smoke.wav','--output',str(downloaded))
        assert digest(downloaded)==digest(self.fixture/'db/assets/local smoke.wav')
        from urllib.parse import urlsplit
        asset_query=urlsplit(link['preview_url']).query
        req=urllib.request.Request(self.server+'/api/public/file?'+asset_query,headers={'Authorization':'Bearer '+self.app_token,'Range':'bytes=0-15'})
        with urllib.request.urlopen(req,timeout=10) as response:
            assert response.status==206 and response.read()==downloaded.read_bytes()[:16]
        self.record('Large asset link, authenticated browser range request, and CLI download')
        plan=self.call('get_plan',{'workflow_id':self.wid})
        step=next(s for s in plan['plan']['steps'] if s['type']=='message_sequence')
        self.original_title=step['title'];self.step_id=step['id']
        result=self.call('update_message_sequence_step',{'workflow_id':self.wid,'expected_revision':plan['revision'],'existing_step_id':self.step_id,'title':self.original_title+' [CLI smoke]','reason':'Verify external CLI on isolated testing workflow.'})
        assert result['revision']!=plan['revision']
        self.call('update_message_sequence_step',{'workflow_id':self.wid,'expected_revision':plan['revision'],'existing_step_id':self.step_id,'title':'stale','reason':'Confirm stale plan rejection.'},expected=4)
        self.record('native plan tool updates real testing step and rejects stale plan',step_id=self.step_id)
        # Generic file writes must not reach protected plans, even for an admin.
        for name,fields in [('write_file',{'content':'{}'}),('patch_file',{'diff':'invalid'})]:
            denial=self.call(name,{'workflow_id':self.wid,'path':'planning/plan.json','expected_revision':'missing',**fields},expected=3)
            assert denial['error']['code']=='protected_path'
        self.record('generic plan write and patch blocked')
        runs=self.call('list_runs',{'workflow_id':self.wid});assert runs['entries']
        logs=self.call('get_logs',{'workflow_id':self.wid,'run_folder':'iteration-smoke/smoke'});assert logs['entries']
        self.record('run and log artifact inspection')
        # Create a genuine read-only account through the existing admin API.
        reader_pw=secrets.token_urlsafe(20)
        http(self.server+'/api/admin/users',{'username':'external-reader','password':reader_pw,'can_create':False,'can_edit':False,'products':['agentworks']},self.app_token,expected=201)
        reader=http(self.server+'/api/auth/login',{'username':'external-reader','password':reader_pw})
        denied=http(self.server+'/api/external/v1/call',{'name':'update_message_sequence_step','arguments':{'workflow_id':self.wid,'expected_revision':result['revision'],'existing_step_id':self.step_id,'title':'no','reason':'Permission check.'}},reader['token'],expected=403)
        assert denied['error']['code']=='forbidden'
        http(self.server+'/api/external/v1/tools',expected=401)
        self.record('real reader credentials denied edits; unauthenticated discovery denied')
    def test_mcp(self):
        errlog=(self.root/'logs/mcp-stderr.log').open('w');self.logs.append(errlog)
        proc=subprocess.Popen([str(self.cli),'--config',str(self.config),'mcp','serve'],env=self.env,cwd=self.root/'runtime',stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=errlog,text=True,bufsize=1,start_new_session=True)
        self.processes.append(proc);seq=0
        def rpc(method,params):
            nonlocal seq;seq+=1
            proc.stdin.write(json.dumps({'jsonrpc':'2.0','id':seq,'method':method,'params':params})+'\n');proc.stdin.flush()
            selector=selectors.DefaultSelector();selector.register(proc.stdout,selectors.EVENT_READ)
            deadline=time.monotonic()+45
            try:
                while time.monotonic()<deadline:
                    if not selector.select(max(0,deadline-time.monotonic())):break
                    line=proc.stdout.readline()
                    if not line:raise RuntimeError('MCP server exited')
                    response=json.loads(line)
                    if response.get('id')==seq:
                        if 'error' in response:raise RuntimeError(str(response['error']))
                        return response['result']
                raise TimeoutError('MCP response timed out')
            finally:selector.close()
        rpc('initialize',{'protocolVersion':'2025-11-25','capabilities':{},'clientInfo':{'name':'agentworks-local-smoke','version':'1'}})
        proc.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n');proc.stdin.flush()
        catalog=rpc('tools/list',{});assert any(t['name']=='get_plan' for t in catalog['tools'])
        def mcp_call(name,args):
            result=rpc('tools/call',{'name':name,'arguments':args})
            if result.get('isError'):raise RuntimeError(str(result))
            return result.get('structuredContent') or json.loads(result['content'][0]['text'])
        asset=mcp_call('get_file_link',{'workflow_id':self.wid,'path':'db/assets/local smoke.wav'})
        assert asset['size']>2<<20 and '/file?path=' in asset['preview_url'] and 'token=' not in asset['preview_url']
        self.record('MCP asset link returns the browser preview and large-file metadata')
        current=mcp_call('get_plan',{'workflow_id':self.wid})
        mcp_call('update_message_sequence_step',{'workflow_id':self.wid,'expected_revision':current['revision'],'existing_step_id':self.step_id,'title':self.original_title,'reason':'Restore isolated testing step title through MCP after CLI smoke test.'})
        restored=self.call('get_plan',{'workflow_id':self.wid});assert next(s for s in restored['plan']['steps'] if s['id']==self.step_id)['title']==self.original_title
        self.record('real stdio MCP handshake/discovery/plan mutation, verified by CLI')
        changelogs=list((self.fixture/'planning/changelog').glob('*.json'));assert changelogs
        text='\n'.join(p.read_text() for p in changelogs)
        assert 'Verify external CLI' in text and 'Restore isolated testing' in text
        self.record('both CLI and MCP changes recorded in native changelog')
        req=urllib.request.Request(self.server+'/api/auth/access-tokens/'+self.pat_id,method='DELETE',headers={'Authorization':'Bearer '+self.app_token})
        with urllib.request.urlopen(req,timeout=10) as response: assert response.status==204
        denial=self.command('tools','list',expected=3);assert denial['error']['code']=='invalid_token'
        denied=rpc('tools/call',{'name':'get_plan','arguments':{'workflow_id':self.wid}});assert denied.get('isError')
        self.record('Revocation rejects saved CLI credentials and an already-connected MCP bridge')
    def finish(self,error=None):
        for proc in reversed(self.processes):
            if proc.poll() is None:
                proc.terminate()
                try:proc.wait(timeout=25)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid,signal.SIGKILL);proc.wait(timeout=5)
        for log in self.logs:log.close()
        unchanged={p:digest(SOURCE/p)==v for p,v in self.before.items()}
        receipt={'source':str(SOURCE),'source_hashes':self.before,'source_inputs_unchanged':all(unchanged.values()),
         'fixture':str(self.fixture),'scope':'built CLI and stdio MCP against real isolated services; no workflow/model execution',
         'model_calls':0,'workflow_runs':0,'services_stopped':all(p.poll() is not None for p in self.processes),
         'results':self.results,'passed':error is None and all(unchanged.values()),'error':None if error is None else str(error)}
        (self.root/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
        # Credentials and ephemeral authority are no longer useful after shutdown.
        self.config.unlink(missing_ok=True)
        print('Receipt:',self.root/'receipt.json',flush=True)
        if not all(unchanged.values()):raise RuntimeError('Source workflow changed during test')

def main():
    probe=Probe();print('Artifacts:',probe.root,flush=True);error=None
    try:probe.prepare();probe.build();probe.launch();probe.test_cli();probe.test_mcp()
    except Exception as e:error=e;print('FAIL:',e,flush=True)
    finally:probe.finish(error)
    if error:raise SystemExit(1)
if __name__=='__main__':main()
