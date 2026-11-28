package provider

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/attr"
)

// attrValue lets a list of framework values be built from a slice of any value type.
type attrValue = attr.Value

// protocolPattern is a plugin protocol version such as 5.0 or 6.0.
var protocolPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
