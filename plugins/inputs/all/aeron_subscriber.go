//go:build !custom || inputs || inputs.aeron_subscriber

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/aeron_subscriber" // register plugin
