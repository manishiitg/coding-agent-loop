agent builder task

till now we have, tried to create a plan automatically via LLM. 
in workflow we try to optimize the plan but thats expensive and goes in different directions.


i want human assisted plan generation. like the validation, data criqueie will be human instead of llm. 

LLM will only do plan writing , and execution.  but the UI should be such that its human assisted.

we need to put less on the human and make thi process more automated.


will update the existing 


1. i have an objective, i should be able to create a flow for it using llm + human input

2. we should start and LLM should generate a plan and display to the user?

3.a LLM creates a simple step wise plan
3.b maybe we break the steps in parts that can be executed

3.c we create multiple agents for every step? automatically with each having todo as the oubjective..

3.d we ask user to confirm the plan? 

3.d. human can update the objective? manually?

3.c human can add steps in between?

3.d. human runs it ? and its clear which agent is running? 