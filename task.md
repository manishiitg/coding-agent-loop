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

h) should we conditional llm, manually to change flow? like during scrapping. if here is a logout or wrong password there is a critical error.

the prompts we have, have variable issue which we get to know later when we run. 

2. variable looping agent or variable injection agent
3. step deciding agent
4. chain builder?
6. have a different knowledge tools and a different directly fully for knowledge/ which will index data
i) prompt tempaltes, need to check variables on compile time

k) also fix logging in this process

human review tool doesn't work with code exec

h) check if external package is even required

i) check where the generated/ folder should be. if we independely use mcpagent. and also write examples for both llmprovider/mcpagent

k) check if we emit specific emivents from backend so that we can highligh proper file in workspace

56) we should have a option in validation agent to migrate to another step