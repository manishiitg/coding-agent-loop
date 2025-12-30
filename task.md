9. see if we can create mock llm with tools calls which we can use for tests 

12. agent should support streaming

13. review conversation.go why is it so big and the retry function

23. check history management for agent.. does it have tool calls also.. like if we cancel inbetween and ask it something

30. test stopping of LLM in between lie cursor and see if follows flows

35. we need to have a background mode else we should kill   the agent

44. we need a way to see logs for mcp installation and tool testing

53. the diff tool in workspace, has some issues. test with json format
also fails in writing python code due to indentation

new tasks

f) add hallunication check as extra agent
g) add references as extra agent 


4. chain builder?
6. have a different knowledge tools and a different directly fully for knowledge/ which 

will index data
i) prompt tempaltes, need to check variables on compile time


11. 📂 List Workspace Files • Turn: 1 • Server: custom
11:01:01
📁 Folder:
planning/changelog
📏 Max Depth:.. if there a single path only.. can we prefix auto?

16. diff patch tool, corrupts json

21. see how we can integrate learning locking and make it more automatic

22. we should have a human notification tool also.


28. need to simply tool filter.

32. remove locking learnings in orchestrator, it should always learn.. or maybe we need thinkg of learnins for orchestrator differently?
26. learnings vs skills? 
27. workspace, sheel exec is reading env variable which is an issue

28.. the context eidting/ summiazation, token call, we do after a turn.. and not after a tool call

29.. get credit card, mutual funds, itr complete finacal info
29. we cannot always, delete all execution when starting a research agent might want to reuse execution

30. refractor how llm are setup and llm fallback

31. high the discover page.. not required

32. delete memory/

33. check scripts/'

34. generated/ is pushed to repo

34. why is llm-providers/ at top level of repo?

35. fix tests most not relevent anymore

36. rename toto create human to a better name

37. auto choose models based on llm and generation?

38. plan debugging, does do changelogs?

39. check learning for validation failure in a loop

40. completely decouple chat from workspace.. right now mcp tool/context comes from workspace.

41. the iteration/eecution settings etc should be workflow specific right now its shared

42. when we update variables,, the entire plan canvas renders.. so its not smooth at all  -done

43. with latest changes, auto highlight of worksace stoppped working

44. before running,, create empty learning folder as agent always checks  -done

1. High Complexity in Retry Loop
File: agent/llm_generation.go:368
Issue: High cyclomatic complexity in GenerateContentWithRetry.
Recommendation: Refactor retry logic into a separate strategy function.


45. the plan step optimization should optimize, max turns, context editing, sumiization etc

46. we need to some unlock learnings when plan updates and able to converge fast

47. if we have failure learning, no point in doing detection ? because in the end we are going call success -done

48. in ui its not clear how many times validation has failed