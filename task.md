7. remove langfuse from core of the system and bring it at external package

9. see if we can create mock llm with tools calls which we can use for tests 

12. agent should support streaming

13. review conversation.go why is it so big and the retry function

15. how to cleanup tool output folder

23. check history management for agent.. does it have tool calls also.. like if we cancel inbetween and ask it something

30. test stopping of LLM in between lie cursor and see if follows flows

35. we need to have a background mode else we should kill   the agent

44. we need a way to see logs for mcp installation and tool testing

51. get resource, doesn't work test with google-sheets

53. the diff tool in workspace, has some issues. test with json format
also fails in writing python code due to indentation

new tasks

f) add hallunication check as extra agent
g) add references as extra agent 


4. chain builder?
6. have a different knowledge tools and a different directly fully for knowledge/ which 

will index data
i) prompt tempaltes, need to check variables on compile time

k) also fix logging in this process

h) check if external package is even required


5. Checking if tab exists in spreadsheet...
ListSheets output: Error: Error executing tool list_sheets: [Errno 32] Broken pipe
❌ Error listing sheets: Error: Error executing tool list_sheets: [Errno 32] Broken pipe === we have handled this properly.. but in mcp code exec, lets add retry