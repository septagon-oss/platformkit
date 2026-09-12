package platformkit.task

import rego.v1

test_policy_decisions[case.name] if {
	some case in data.task_policy_cases
	actual := allowed with input as case.input
	actual == case.expected
}
