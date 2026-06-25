64. make multiagent like highly memory orientated having access to workflows. - progress

66. we need have a total to notify human based on connectors

68. implement agent sdk closer to 15th june when claude will have the plan for it 

68. optimize Pi.dev routing for kimi, glm, minimax, deepseek, free plans, and Go plan

69. also to make onboarding easily.. most users might start with a single coding plan cli.. so need to make it easy to start chat and workflows ... maybe by setting some default cli right now user needs to manually set llm always

70. right now every tool call we make, it is like has auth token, 


• Called
  └ api-bridge.execute_shell_command({"command":"set -euo pipefail\nROOT=\"/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Workflow/social-
        media\"\nprintf '--- soul/soul.md ---\\n'\nif [ -s \"$ROOT/soul/soul.md\" ]; then cat \"$ROOT/soul/soul.md\"; else echo 'MISSING_OR_EMPTY';
        fi\nprintf '\\n--- foundation sections ---\\n'\nif [ -f \"$ROOT/soul/soul.md\" ]; then grep -E '^## (Objective|Success Criteria)' \"$ROOT/soul/
        soul.md\" || true; fi\nprintf '\\n--- plan exists / top-level steps ---\\n'\nif [ -f \"$ROOT/planning/plan.json\" ]; then jq '[.steps[] | {id, title,
        type}]' \"$ROOT/planning/plan.json\"; else echo 'NO_PLAN'; fi\nprintf '\\n--- successful run indicators ---\\n'\nif [ -d \"$ROOT/runs\" ]; then find
        \"$ROOT/runs\" -maxdepth 4 -type f \\( -name 'run.json' -o -name 'metadata.json' -o -name 'result.json' -o -name 'status.json' \\) -print | head -50;
        else echo 'NO_RUNS_DIR'; fi\nprintf '\\n--- variables/variables.json ---\\n'\nif [ -f \"$ROOT/variables/variables.json\" ]; then cat \"$ROOT/
        variables/variables.json\"; else echo 'NO_VARIABLES'; fi\nprintf '\\n--- recent builder conversations ---\\n'\nif [ -d \"$ROOT/builder\" ]; then ls
        -t \"$ROOT\"/builder/*.json 2>/dev/null | head -3; else echo 'NO_BUILDER_DIR'; fi","timeout":30})
    {"stdout": "", "stderr": "sh: line 2: printf: --: invalid option\nprintf: usage: printf [-v var] format [arguments]\n", "exit_code": 2,
        "execution_time_ms": 16}


71. can we show the recap from claude code and show that in resume?

72. add goal support

73. should auto notification also send if faced any issues in execution it tasks to the main buider so main builder can fix it ?

74. Pi.dev integration follow-up: add curated model picker/provider routing so users see recommended choices first (for example `google/gemini-3.5-flash`) and can search the full Pi model catalog only when needed.
