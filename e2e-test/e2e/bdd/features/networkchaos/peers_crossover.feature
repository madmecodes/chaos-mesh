Feature: NetworkChaos - Peers Crossover

  # Regression for https://github.com/chaos-mesh/chaos-mesh/issues/1450:
  # both-direction chaos between two partitions must not affect within-partition links.

  Background:
    Given the network peers are ready and all connections are good

  Scenario: Delay between partition 0 and partition 1 leaves within-partition links alone
    When I inject both-direction delay between partition "0" and partition "1"
    Then slow connections should be peer pairs [[0,1],[0,3],[1,2],[2,3]] within 15 seconds
    When I delete the NetworkChaos "network-chaos-1"
    Then all connections should recover within 15 seconds
