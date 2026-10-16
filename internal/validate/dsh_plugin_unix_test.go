//go:build !windows

package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/host"
)

func TestDshControlToolsUseOriginalFormAndHumanSelection(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	root := t.TempDir()
	launcher := filepath.Join(root, ".local", "bin", "formal-gates")
	mustWriteValidateTest(t, launcher, `#!/usr/bin/env node
const {createInterface}=require('node:readline')
const {appendFileSync}=require('node:fs')
if(JSON.stringify(process.argv.slice(2))!==JSON.stringify(['workflow','serve']))throw Error('wrong command')
let initialized=false,ready=false,call
const send=m=>process.stdout.write(JSON.stringify({jsonrpc:'2.0',...m})+'\n')
createInterface({input:process.stdin}).on('line',line=>{
 const m=JSON.parse(line)
 if(m.method==='initialize') {
  if(!m.params.capabilities.elicitation.form)throw Error('missing form capability')
  initialized=true;send({id:m.id,result:{protocolVersion:m.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'formal-gates',version:'1'}}})
 } else if(m.method==='notifications/initialized'){ready=true
 } else if(m.method==='tools/list'){
  if(!initialized||!ready)throw Error('not initialized')
  send({id:m.id,result:{tools:[{name:'confirm_control',description:'Original control',inputSchema:{type:'object',properties:{root:{type:'string'},candidate:{type:'integer'}},required:['candidate'],additionalProperties:false}}]}})
 } else if(m.method==='tools/call'){
  if(!initialized||!ready||m.params.name!=='confirm_control'||m.params.arguments.candidate!==3)throw Error('wrong source call')
  const owner=m.params._meta?.['formal-gates/owner']
  if(!owner||owner.session!=='actual-root'||owner.transcript!==process.env.HOME+'/owner.jsonl'||owner.provider!=='deepseek-harness'||'owner'in m.params.arguments)throw Error('original host owner lost')
  call=m;send({id:'human',method:'elicitation/create',params:{mode:'form',message:'Exact bound operation for candidate 3',requestedSchema:{type:'object',properties:{confirmOperation:{type:'boolean'}},required:['confirmOperation'],additionalProperties:false}}})
 } else if(m.id==='human'){
  appendFileSync(process.env.CAPTURE_RESPONSES,JSON.stringify({arguments:call.params.arguments,metadata:call.params._meta,response:m.result??m.error})+'\n')
  if(m.result?.action==='accept'&&m.result.content?.confirmOperation!==true)throw Error('missing original confirmation field')
  send({id:call.id,result:{content:[{type:'text',text:m.result?.action??'error'}],isError:!!m.error}})
 }
})
`)
	if err := os.Chmod(launcher, 0700); err != nil {
		t.Fatal(err)
	}
	plugin, err := filepath.Abs("dsh_plugin.mjs")
	if err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(root, "driver.mjs")
	mustWriteValidateTest(t, driver, `import {pathToFileURL} from 'node:url'
const [plugin,root]=process.argv.slice(2)
const {apply}=await import(pathToFileURL(plugin))
let setup,tool,selection,asked=0,lateResolve
const agent={session:{header:{id:'actual-root',cwd:root}}}
const control={
 tools:{register(value){tool=value;return()=>{}}},effect(){},
 get(name){if(name!=='sessionPersistence')throw Error(name);return{locate(header){if(header!==agent.session.header)throw Error('wrong session header');return{path:root+'/owner.jsonl'}}}},
 userQuestions:{async ask(request){
  if(request.agent!==agent||request.questions[0].question!=='Exact bound operation for candidate 3')throw Error('original binding lost')
  if(request.questions[0].options.some(o=>'default'in o||'selected'in o))throw Error('preselected answer')
  asked++
  if(selection==='wait')return new Promise(resolve=>{lateResolve=resolve})
  return {answers:[{id:'confirmation',selected:selection==='custom'?[]:[selection],custom:selection==='custom'?'确认并执行':undefined}]}
 }}
}
apply({logger:{warn(){}},on(){},inject(names,callback){if(names.join(',')!=='tools,userQuestions')throw Error('wrong native services');setup=callback(control)}},{provider:'deepseek-harness'})
await setup
if(tool.name!=='confirm_control'||tool.parameters.required[0]!=='candidate')throw Error('schema not discovered')
for(const [choice,want] of [['确认并执行','accept'],['取消','decline'],['custom','decline']]){
 selection=choice
 const result=await tool.execute({candidate:3},{agent,signal:new AbortController().signal})
 if(result.content[0].text!==want)throw Error('choice changed')
}
selection='wait'
const abort=new AbortController()
const pending=tool.execute({candidate:3},{agent,signal:abort.signal})
while(!lateResolve)await new Promise(resolve=>setImmediate(resolve))
abort.abort()
try{await pending;throw Error('cancelled invocation completed')}catch(error){if(!String(error).includes('cancel'))throw error}
lateResolve({answers:[{id:'confirmation',selected:['确认并执行']}]})
await new Promise(resolve=>setImmediate(resolve))
if(asked!==4)throw Error('wrong native interaction count')
`)
	capture := filepath.Join(root, "responses.jsonl")
	cmd := exec.Command(node, driver, plugin, root)
	cmd.Env = append(os.Environ(), "HOME="+root, "CAPTURE_RESPONSES="+capture)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("control plugin protocol failed: %v: %s", err, output)
	}
	body, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 3 {
		t.Fatalf("cancelled or extra response reached service: %s", body)
	}
	for index, line := range lines {
		var record struct {
			Metadata struct {
				Owner struct {
					Session    string `json:"session"`
					Transcript string `json:"transcript"`
					Provider   string `json:"provider"`
				} `json:"formal-gates/owner"`
			} `json:"metadata"`
			Arguments struct {
				Root      string `json:"root"`
				Candidate int    `json:"candidate"`
			} `json:"arguments"`
			Response struct {
				Action  string `json:"action"`
				Content struct {
					ConfirmOperation bool `json:"confirmOperation"`
				} `json:"content"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil || record.Arguments.Root != root || record.Arguments.Candidate != 3 {
			t.Fatalf("changed root or candidate: %v %s", err, line)
		}
		if record.Metadata.Owner.Session != "actual-root" || record.Metadata.Owner.Transcript != filepath.Join(root, "owner.jsonl") || record.Metadata.Owner.Provider != host.DeepSeek {
			t.Fatalf("changed current host owner: %s", line)
		}
		want := "decline"
		if index == 0 {
			want = "accept"
		}
		if record.Response.Action != want || record.Response.Content.ConfirmOperation != (index == 0) {
			t.Fatalf("changed actual human selection: %s", line)
		}
	}
}
