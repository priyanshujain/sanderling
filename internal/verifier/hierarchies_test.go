package verifier

const listHierarchyXML = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" package="test.app" bounds="[0,0][1080,2400]">
    <node index="0" class="android.widget.TextView" text="Items" bounds="[100,200][900,300]" />
    <node index="1" class="android.view.View" content-desc="primary_action" clickable="true" enabled="true" bounds="[64,2200][1016,2320]" />
    <node index="2" class="android.view.View" content-desc="secondary_action" clickable="true" enabled="true" bounds="[980,80][1060,160]" />
  </node>
</hierarchy>`

const formHierarchyXML = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" package="test.app" bounds="[0,0][1080,2400]">
    <node index="0" class="android.view.View" content-desc="text_field" clickable="true" enabled="true" bounds="[64,320][1016,440]" />
    <node index="1" class="android.view.View" content-desc="primary_action" clickable="true" enabled="true" bounds="[64,2200][1016,2320]" />
    <node index="2" class="android.view.View" content-desc="secondary_action" clickable="true" enabled="true" bounds="[32,80][112,160]" />
  </node>
</hierarchy>`
