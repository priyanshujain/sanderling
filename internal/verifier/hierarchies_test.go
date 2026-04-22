package verifier

const listHierarchyJSON = `{
  "attributes": {"class": "android.widget.FrameLayout", "package": "test.app", "bounds": "[0,0,1080,2400]"},
  "children": [
    {"attributes": {"class": "android.widget.TextView", "text": "Items", "bounds": "[100,200,900,300]"}, "children": []},
    {"attributes": {"content-desc": "primary_action", "bounds": "[64,2200,1016,2320]"}, "children": [], "clickable": true, "enabled": true},
    {"attributes": {"content-desc": "secondary_action", "bounds": "[980,80,1060,160]"}, "children": [], "clickable": true, "enabled": true}
  ]
}`

const formHierarchyJSON = `{
  "attributes": {"class": "android.widget.FrameLayout", "package": "test.app", "bounds": "[0,0,1080,2400]"},
  "children": [
    {"attributes": {"content-desc": "text_field", "bounds": "[64,320,1016,440]"}, "children": [], "clickable": true, "enabled": true},
    {"attributes": {"content-desc": "primary_action", "bounds": "[64,2200,1016,2320]"}, "children": [], "clickable": true, "enabled": true},
    {"attributes": {"content-desc": "secondary_action", "bounds": "[32,80,112,160]"}, "children": [], "clickable": true, "enabled": true}
  ]
}`
