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

1. we need add a UI to edit the plan json manually

f) add hallunication check as extra agent
g) add references as extra agent 
h) should we conditional llm, manually to change flow? like during scrapping. if here is a logout or wrong password there is a critical error.
i) learning agent for overall run as new mode


the prompts we have, have variable issue which we get to know later when we run. 

