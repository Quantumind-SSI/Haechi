# Haechi device authorisation policy (single source of truth for the OPA
# adapter, adapters/opa). Deny-by-default; the StaticEngine dev adapter encodes
# the identical rule.
#
# Query: data.haechi.authz.decision with input = AuthzInput.
package haechi.authz

import rego.v1

# Default decision is a hard deny (fail closed).
default decision := {"allow": false, "reason": "deny-by-default"}

decision := {"allow": true, "reason": "ok"} if {
	not input.device_revoked
	some c in input.allowed_service_classes
	c == input.service_class
}

decision := {"allow": false, "reason": "device revoked"} if {
	input.device_revoked
}

decision := {"allow": false, "reason": "service class not permitted"} if {
	not input.device_revoked
	not service_class_allowed
}

service_class_allowed if {
	some c in input.allowed_service_classes
	c == input.service_class
}
