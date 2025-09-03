//go:build !custom || outputs || outputs.aeron_publisher

package all

import _ "github.com/influxdata/telegraf/plugins/outputs/aeron_publisher" // register plugin
