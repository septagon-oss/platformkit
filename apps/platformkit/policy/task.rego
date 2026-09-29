# The reference application's object-scope rules for tasks (decision 0011, question 2).
#
# The task module asks before it assigns or resolves, with facts it loaded inside its own
# transaction (input.resource.attributes: status, priority, assignee_id, and for an
# assignment requested_assignee_id) and the signed-in person as input.actor. Whether the
# person holds task:assign or task:resolve at all was answered before the module ran; this
# is the question a grant cannot answer — which task.
#
# One rule this composition adds: an assigned task is its assignee's to resolve. The
# service already refuses assigning a resolved or closed task, so assignment is allowed
# here and that refusal stays the module's own. A client that wants other rules writes
# its own policy; the module does not change.
package platformkit.task

default decision := {"allow": false, "reason": "no rule allows this action on a task"}

decision := {"allow": true, "reason": "a holder of task:assign may assign a task"} if {
	input.action == "task:assign"
}

decision := {"allow": true, "reason": "an unassigned task may be resolved by a holder of task:resolve"} if {
	input.action == "task:resolve"
	input.resource.attributes.assignee_id == ""
}

decision := {"allow": true, "reason": "the assignee resolves the task assigned to them"} if {
	input.action == "task:resolve"
	input.resource.attributes.assignee_id == input.actor.id
}

decision := {"allow": false, "reason": "an assigned task is resolved by its assignee"} if {
	input.action == "task:resolve"
	input.resource.attributes.assignee_id != ""
	input.resource.attributes.assignee_id != input.actor.id
}
