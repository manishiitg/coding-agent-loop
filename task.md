9. see if we can create mock llm with tools calls which we can use for tests 

12. agent should support streaming

13. review conversation.go why is it so big and the retry function

44. we need a way to see logs for mcp installation and tool testing - done

53. the diff tool in workspace, has some issues. test with json format
also fails in writing python code due to indentation - done

new tasks

f) add hallunication check as extra agent
g) add references as extra agent 


4. chain builder?

will index data
i) prompt tempaltes, need to check variables on compile time


18. we need add folder guard in custom shell exec -- done

21. see how we can integrate learning locking and make it more automatic
-- done

22. we should have a human notification tool also.

32. remove locking learnings in orchestrator, it should always learn.. or maybe we need thinkg of learnins for orchestrator differently? -- done

26. learnings vs skills? 

27. workspace, sheel exec is reading env variable which is an issue -- done

28.. the context eidting/ summiazation, token call, we do after a turn.. and not after a tool call

29.. get credit card, mutual funds, itr complete finacal info
29. we cannot always, delete all execution when starting a research agent might want to reuse execution

30. refractor how llm are setup and llm fallback - done

35. fix tests most not relevent anymore - done

36. rename toto create human to a better name - done

37. auto choose models based on llm and generation?

38. plan debugging, does do changelogs? - done

39. check learning for validation failure in a loop

40. completely decouple chat from workspace.. right now mcp tool/context comes from workspace. - done

41. the iteration/eecution settings etc should be workflow specific right now its shared - done

45. the plan step optimization should optimize, max turns, context editing, sumiization 

48. in ui its not clear how many times validation has failed

49. context eidting/ summarization need that configurable? do we capture costs for summary etc properly?

50. we need to have evals for workflows

51. summization in chat mode is not ux friendly

52. check auto summization.. it trigges many times.. i think some bug is there in token calcuation

53. add oauth support for mcp and make a better ui for adding mcp/managing mcp than simple json ---- done

54. in multi llm package, keep only latest models. and focus on good data for those ---- done

55. langfuse tracing

56. supabase migration

57. can we create a test propmt in openrouter to test how good a model is, mainly in agentic behaviour