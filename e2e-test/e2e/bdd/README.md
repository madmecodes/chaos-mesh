# BDD e2e tests

Godog-based BDD layer for the Chaos Mesh e2e suite. Runs alongside the existing
Ginkgo tests.

## Layout

```
features/    Gherkin scenarios
steps/       step definitions
suite_test.go    Godog test runner
```

## Run

```bash
cd e2e-test
go test ./e2e/bdd/... -v --kubeconfig=$HOME/.kube/config --namespace=chaos-testing
```

## Adding scenarios

1. Edit a `.feature` file under `features/`.
2. Run the tests; Godog prints stubs for any unimplemented steps.
3. Implement them in the matching `steps/*_steps.go` file.

Steps reuse the existing `pkg/fixture` and `e2e/util` helpers, so most new
scenarios only need a few lines.
