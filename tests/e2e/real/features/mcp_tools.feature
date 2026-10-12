Feature: MCP tools over the real MCP server
  An MCP client drives the platform through the real server (agent tools plus
  the in-process operator adapter) backed by the real usecases.

  Scenario: The tool surface exposes the operator tools and none of the REST-only ones
    Given a script strategy "Trader" exists
    And the MCP client is connected
    Then the MCP tool list contains "get_settings"
    And the MCP tool list contains "create_agent"
    And the MCP tool list contains "update_agent"
    And the MCP tool list contains "delete_agent"
    And the MCP tool list contains "run_agent"
    And the MCP tool list contains "set_kill_switch"
    And the MCP tool list contains "list_webhook_targets"
    And the MCP tool list contains "run_backtest"
    And the MCP tool list contains "run_walk_forward"
    And the MCP tool list contains "run_monte_carlo"
    And the MCP tool list contains "get_backtest_report_url"
    And the MCP tool list contains "get_agent_report_url"
    And the MCP tool list has no tool matching "(?i)(update|put|set)_(settings|account|mode|status|deploy_gate)"
    And the MCP tool list has no tool matching "(?i)(close_position|close_signal|approve|reject)"
    And the MCP tool list has no tool matching "(?i)(create|update|delete|test)_webhook"
    And the MCP tool list has no tool matching "(?i)(users?|login|token)"

  Scenario: get_settings never leaks credentials, tokens or webhook URLs
    Given settings with broker credentials and a webhook URL are stored
    And the MCP client is connected
    When I call MCP tool "get_settings"
    Then the MCP call succeeds
    And the MCP result JSON "mode" is "dryrun"
    And the MCP result does not contain "AKIA-LIVE-KEY"
    And the MCP result does not contain "LIVE-SECRET-VALUE"
    And the MCP result does not contain "TESTNET-KEY"
    And the MCP result does not contain "TESTNET-SECRET"
    And the MCP result does not contain "hooks.example.com"
    And the MCP result does not contain "bearer-token-abc"
    And the MCP result does not contain "••••"

  Scenario: Full agent lifecycle, including patch semantics and the kill switch
    Given the MCP client is connected
    When I call MCP tool "create_agent" with:
      """
      {"name":"Nightly review","goal":"Review the strategies","permissions":["read"],"triggers":{"cron":["0 3 * * *"]},"daily_budget_usd":2}
      """
    Then the MCP call succeeds
    And I remember MCP result JSON "id" as "agent"
    When I call MCP tool "update_agent" with:
      """
      {"agent_id":{agent},"goal":"Review only BTC"}
      """
    Then the MCP call succeeds
    When I call MCP tool "get_agent" with:
      """
      {"agent_id":{agent}}
      """
    Then the MCP result JSON "goal" is "Review only BTC"
    And the MCP result JSON "name" is "Nightly review"
    And the MCP result JSON "triggers.cron.0" is "0 3 * * *"
    And the MCP result JSON "daily_budget_usd" is "2"
    When I call MCP tool "run_agent" with:
      """
      {"agent_id":{agent},"prompt":"look at BTC"}
      """
    Then the MCP call succeeds
    And an agent run was enqueued for agent "{agent}"
    When I call MCP tool "set_kill_switch" with:
      """
      {"paused":true}
      """
    Then the MCP result JSON "agents_paused" is "true"
    When I call MCP tool "get_kill_switch"
    Then the MCP result JSON "agents_paused" is "true"
    When I call MCP tool "set_kill_switch" with:
      """
      {"paused":false}
      """
    Then the MCP result JSON "agents_paused" is "false"
    When I call MCP tool "delete_agent" with:
      """
      {"agent_id":{agent}}
      """
    Then the MCP call succeeds
    When I call MCP tool "get_agent" with:
      """
      {"agent_id":{agent}}
      """
    Then the MCP call fails

  Scenario: The kill switch stops run_agent
    Given the MCP client is connected
    When I call MCP tool "create_agent" with:
      """
      {"name":"Guarded","goal":"g","permissions":["read"]}
      """
    And I remember MCP result JSON "id" as "agent"
    And I call MCP tool "set_kill_switch" with:
      """
      {"paused":true}
      """
    And I call MCP tool "run_agent" with:
      """
      {"agent_id":{agent}}
      """
    Then the MCP call fails
    And the MCP result contains "kill switch"
    And no agent run was enqueued

  Scenario: Agent validation errors come back as tool errors
    Given the MCP client is connected
    When I call MCP tool "create_agent" with:
      """
      {"name":"Broken","goal":"g","permissions":["read"],"triggers":{"cron":["not a cron"]}}
      """
    Then the MCP call fails
    And the MCP result contains "cron"

  Scenario: run_backtest is asynchronous over MCP and the report link opens in a browser
    Given a script strategy "Trader" exists
    And 10 days of BTCUSDT 1h candles exist from "2025-01-01"
    And the MCP client is connected
    When I call MCP tool "run_backtest" with:
      """
      {"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2025-01-02T00:00:00Z","end_date":"2025-01-08T00:00:00Z"}
      """
    Then the MCP call succeeds
    And the MCP result contains "status=queued"
    And I remember MCP result matching "backtest_id=(\d+)" as "bt"
    And 1 backtest task is queued
    When I call MCP tool "get_backtest_report_url" with:
      """
      {"backtest_id":{bt}}
      """
    Then the MCP call fails
    And the MCP result contains "queued"
    When the worker processes the queue
    And I call MCP tool "get_backtest" with:
      """
      {"backtest_id":{bt}}
      """
    Then the MCP result does not contain "Not finished yet"
    When I call MCP tool "get_backtest_report_url" with:
      """
      {"backtest_id":{bt},"lang":"pt-BR"}
      """
    Then the MCP call succeeds
    When I GET the URL in MCP result JSON "url"
    Then the response status is 200
    And the response body contains "<html"

  Scenario: run_walk_forward queues and the worker completes the same run
    Given a script strategy "Trader" exists
    And 125 days of BTCUSDT 1h candles exist from "2025-01-01"
    And the MCP client is connected
    When I call MCP tool "run_walk_forward" with:
      """
      {"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2025-01-01","end_date":"2025-05-02","train_months":3,"test_months":1}
      """
    Then the MCP call succeeds
    And the MCP result JSON "status" is "queued"
    And I remember MCP result JSON "backtest_id" as "bt"
    And 1 backtest task is queued
    When the worker processes the queue
    And I call MCP tool "get_backtest" with:
      """
      {"backtest_id":{bt}}
      """
    Then the MCP result does not contain "Not finished yet"
    And there is 1 backtest run stored

  Scenario: write_report returns a link and get_agent_report_url opens the same report
    Given a script strategy "Trader" exists
    And the MCP client is connected
    When I call MCP tool "write_report" with:
      """
      {"title":"Weekly review","severity":"info","strategy_ids":[1],"blocks":[{"type":"summary","data":{"text":"Everything looks stable."}}]}
      """
    Then the MCP call succeeds
    And I remember MCP result JSON "report_id" as "report"
    When I call MCP tool "get_agent_report_url" with:
      """
      {"report_id":{report}}
      """
    Then the MCP call succeeds
    When I GET the URL in MCP result JSON "html_url"
    Then the response status is 200
    And the response body contains "Everything looks stable."
