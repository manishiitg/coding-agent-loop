9. see if we can create mock llm with tools calls which we can use for tests 

12. agent should support streaming

13. review conversation.go why is it so big and the retry function

44. we need a way to see logs for mcp installation and tool testing - done

new tasks

f) add hallunication check as extra agent
g) add references as extra agent 


4. chain builder?

will index data
i) prompt tempaltes, need to check variables on compile time


22. we should have a human notification tool also.

26. learnings vs skills? 

28.. the context eidting/ summiazation, token call, we do after a turn.. and not after a tool call

29.. get credit card, mutual funds, itr complete finacal info
29. we cannot always, delete all execution when starting a research agent might want to reuse execution


37. auto choose models based on llm and generation?

39. check learning for validation failure in a loop

41. the iteration/eecution settings etc should be workflow specific right now its shared - done

45. the plan step optimization should optimize, max turns, context editing, sumiization 

48. in ui its not clear how many times validation has failed

49. context eidting/ summarization need that configurable? do we capture costs for summary etc properly?

51. summization in chat mode is not ux friendly

52. check auto summization.. it trigges many times.. i think some bug is there in token calcuation

53. add oauth support for mcp and make a better ui for adding mcp/managing mcp than simple json ---- done

55. langfuse tracing

58. run multiple workflows together -- test


2. for a new plan step, we should disable validation by default

3. all workflows don't require a knowledgebase

4. typing in chatinput is very slow

5. make large tool output optional in chat agent

6. remove objective and ask user what to do when creating new plan also

7. can orchestrator decide if need to use tempLLM or normal LLM

8. can orchestrator decide if need to leanr or not skills