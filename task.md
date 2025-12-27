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