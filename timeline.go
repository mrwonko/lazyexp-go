package lazyexp

import (
	"html/template"
	"io"
	"time"
)

var timelineTemplate = template.Must(template.New("timeline").Funcs(
	template.FuncMap{
		"month": func(t time.Time) int {
			return int(t.Month() - 1)
		},
		"ms": func(t time.Time) int {
			return t.Nanosecond() / int(time.Millisecond/time.Nanosecond)
		},
		"height": func(fn FlattenedNodes) int {
			return len(fn) * 60
		},
	},
).Parse(`<html>
	<head>
		<script type="text/javascript" src="https://www.gstatic.com/charts/loader.js"></script>
		<script type="text/javascript">
			google.charts.load('current', {'packages':['timeline']});
			google.charts.setOnLoadCallback(drawChart);
			function drawChart() {
				var container = document.getElementById('timeline');
				var chart = new google.visualization.Timeline(container);
				var dataTable = new google.visualization.DataTable();

				dataTable.addColumn({ type: 'string', id: 'Node' });
				dataTable.addColumn({ type: 'date', id: 'Start' });
				dataTable.addColumn({ type: 'date', id: 'End' });
				dataTable.addRows([
					{{- define "date"}}new Date({{.Year}},{{month .}},{{.Day}},{{.Hour}},{{.Minute}},{{.Second}},{{ms .}}){{end}}
					{{- range $node := .}}
					[ '{{ $node.Description }}', {{template "date" $node.FetchStartTime}}, {{template "date" $node.FetchEndTime}} ],
					{{- end }}
				]);

				chart.draw(dataTable);
			}
		</script>
	</head>
	<body>
		<div id="timeline" style="height: {{height .}}px;"></div>
	</body>
</html>
`))

// Timeline serializes the given FlattenedNodes to an HTML page visualizing the execution using Google Timelines Charts (https://developers.google.com/chart/interactive/docs/gallery/timeline)
func Timeline(wr io.Writer, fn FlattenedNodes) error {
	return timelineTemplate.Execute(wr, fn)
}
