truncate  table orders, signals restart identity cascade;
truncate table strategy_executions restart identity;

update accounts 
set amount = 1000,
	available_orders = 10
