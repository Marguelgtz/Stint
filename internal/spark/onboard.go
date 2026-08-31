package spark

import "sort"

const DefaultDashboardURL = "https://spark-api.marguel-gtz.workers.dev/app"

type Area struct {
	ID               string
	ExpectedEvidence []string
}

var Areas = []Area{{ID: "interactive-control-plane", ExpectedEvidence: []string{"go-vet", "unit-tests"}}, {ID: "vast-compute-provider", ExpectedEvidence: []string{"go-vet", "unit-tests"}}, {ID: "model-runtime", ExpectedEvidence: []string{"go-vet", "unit-tests"}}, {ID: "spark-collaboration", ExpectedEvidence: []string{"go-vet", "unit-tests", "spark-profile"}}, {ID: "fallback-inference", ExpectedEvidence: []string{"go-vet", "unit-tests"}}, {ID: "release-and-automation", ExpectedEvidence: []string{"spark-profile", "go-vet", "unit-tests"}}}

type OnboardingPlan struct {
	ProfilePath      string
	DashboardURL     string
	ExpectedEvidence []string
	Steps            []string
}

func CreateOnboardingPlan(dashboardURL string) OnboardingPlan {
	if dashboardURL == "" {
		dashboardURL = DefaultDashboardURL
	}
	unique := map[string]struct{}{}
	for _, area := range Areas {
		for _, evidence := range area.ExpectedEvidence {
			unique[evidence] = struct{}{}
		}
	}
	evidence := make([]string, 0, len(unique))
	for name := range unique {
		evidence = append(evidence, name)
	}
	sort.Strings(evidence)
	return OnboardingPlan{ProfilePath: ".spark/profile.yml", DashboardURL: dashboardURL, ExpectedEvidence: evidence, Steps: []string{"Install the Spark GitHub App for Marguelgtz/Stint.", "Keep .spark/profile.yml committed on the default branch.", "Confirm GitHub Actions emits the expected evidence checks.", "Open a pull request and confirm Spark evaluates the exact head SHA."}}
}
