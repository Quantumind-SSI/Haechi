# Rego unit tests (run with: opa test deploy/opa/policy). A CI release gate
# per Integration Document §5 (OPA: "policy unit tests and deny-by-default").
package haechi.authz

import rego.v1

test_allow_when_class_permitted if {
	decision.allow with input as {
		"device_revoked": false,
		"service_class": "general",
		"allowed_service_classes": ["general"],
	}
}

test_deny_by_default_empty_input if {
	not decision.allow with input as {}
}

test_deny_revoked if {
	not decision.allow with input as {
		"device_revoked": true,
		"service_class": "general",
		"allowed_service_classes": ["general"],
	}
}

test_deny_class_not_permitted if {
	not decision.allow with input as {
		"device_revoked": false,
		"service_class": "premium",
		"allowed_service_classes": ["general"],
	}
}
