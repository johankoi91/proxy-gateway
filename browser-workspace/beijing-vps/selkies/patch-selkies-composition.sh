#!/bin/sh
set -eu

web_root=/usr/local/lib/python3.14/dist-packages/selkies/selkies_web
old='_handleMobileInput(e){let t=e.target;if(this._chordEchoPending()){this._resetAssist();return}let n=t.value||``;(!this.isComposing||n)&&(this._assistTyped=this._streamTextDiff(this._assistTyped,n),this.isComposing||this._resetAssist())}'
new='_handleMobileInput(e){let t=e.target;if(this._chordEchoPending()){this._resetAssist();return}if(this.isComposing)return;let n=t.value||``;this._assistTyped=this._streamTextDiff(this._assistTyped,n),this._resetAssist()}'
clipboard_sender='async function Y(e,t=`text/plain`,n=null){'
clipboard_export='window.lightnodeSendClipboardText=e=>new Promise((t,n)=>{let r=!1;Y(String(e),`text/plain`,e=>{r=!0,n(new Error(e))}).then(()=>{r||t()}).catch(n)});'
matched=0

for file in "$web_root"/src/selkies-core.js "$web_root"/assets/selkies-core-*.js; do
  [ -f "$file" ] || continue
  count="$(OLD="$old" FILE="$file" python3 - <<'PY'
import os

with open(os.environ["FILE"], "r", encoding="utf-8") as handle:
    print(handle.read().count(os.environ["OLD"]))
PY
)"
  [ "$count" -eq 1 ] || {
    echo "expected one Selkies composition handler in $file, found $count" >&2
    exit 1
  }
  OLD="$old" NEW="$new" FILE="$file" python3 - <<'PY'
import os

path = os.environ["FILE"]
with open(path, "r", encoding="utf-8") as handle:
    source = handle.read()
patched = source.replace(os.environ["OLD"], os.environ["NEW"], 1)
with open(path, "w", encoding="utf-8") as handle:
    handle.write(patched)
PY
  export_count="$(CLIPBOARD_SENDER="$clipboard_sender" FILE="$file" python3 - <<'PY'
import os

with open(os.environ["FILE"], "r", encoding="utf-8") as handle:
    print(handle.read().count(os.environ["CLIPBOARD_SENDER"]))
PY
)"
  [ "$export_count" -eq 1 ] || {
    echo "expected one Selkies clipboard sender in $file, found $export_count" >&2
    exit 1
  }
  CLIPBOARD_SENDER="$clipboard_sender" CLIPBOARD_EXPORT="$clipboard_export" FILE="$file" python3 - <<'PY'
import os

path = os.environ["FILE"]
with open(path, "r", encoding="utf-8") as handle:
    source = handle.read()
needle = os.environ["CLIPBOARD_SENDER"]
patched = source.replace(needle, os.environ["CLIPBOARD_EXPORT"] + needle, 1)
with open(path, "w", encoding="utf-8") as handle:
    handle.write(patched)
PY
  matched=$((matched + 1))
done

[ "$matched" -eq 2 ] || {
  echo "expected to patch source and bundled Selkies core, patched $matched files" >&2
  exit 1
}
