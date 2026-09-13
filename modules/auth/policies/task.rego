package platformkit.task

import rego.v1

# An opt-in example: a member may claim work for themselves and resolve their
# own assignments. Route permissions and domain transition rules still apply.
default allowed := false

request := input.resource
attributes := request.resource.attributes

tenant_user if {
	request.tenant.id != ""
	request.resource.tenant_id == request.tenant.id
	request.resource.kind == "task"
	request.resource.id != ""
	request.actor.kind == "user"
	request.actor.id != ""
}

allowed if {
	tenant_user
	request.action == "task:assign"
	attributes.status in {"open", "acknowledged", "in_progress"}
	attributes.requested_assignee_id == request.actor.id
	attributes.assignee_id in {"", request.actor.id}
}

allowed if {
	tenant_user
	request.action == "task:resolve"

	# A repeat resolution still reaches the domain's idempotency/conflict check.
	attributes.status in {"open", "acknowledged", "in_progress", "resolved"}
	attributes.assignee_id == request.actor.id
}
