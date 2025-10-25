7. remove langfuse from core of the system and bring it at external package

9. see if we can create mock llm with tools calls which we can use for tests 

10. remove the filesystem mcp tool

12. agent should support streaming

13. review conversation.go why is it so big and the retry function

15. how to cleanup tool output folder

16. publish opensource and also remove server.go from core package na use external

21. in cursor it calls a tool, then explains itself then again calls a tool.. it doens't happen for us?

23. check history management for agent.. does it have tool calls also.. like if we cancel inbetween and ask it something

25. right now if a single agent doesn't connect.. the entire agent fails

30. test stopping of LLM in between lie cursor and see if follows flows

31.. if i stop and charage query etc it works in UI?

35. we need to have a background mode else we should kill   the agent

41.. can we add a like a user input context to the current execution step.. without actually stopping? 
like pass this from frontend to a variable and when we are sending a user message to LLM.. we just add that .. 

44. we need a way to see logs for mcp installation and tool testing


51. get resource, doesn't work test with google-sheets

52. add support for docker in install of mcp

53. the diff tool in workspace, has some issues. test with json format

54. in presets we should be able to select mcp server tools also to expose