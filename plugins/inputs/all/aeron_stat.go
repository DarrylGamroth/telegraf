//go:build !custom || inputs || inputs.aeron_stat

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/aeron_stat" // register plugin
