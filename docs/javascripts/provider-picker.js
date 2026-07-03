/* =============================================================================
   Interactive provider picker (IRO-311) — progressive enhancement.

   Two jobs, both optional:
     1. Panel switching. Adds `.pp-js` to each picker so CSS hides all but the
        selected panel, then swaps the active panel on radio change. Without this
        file every panel is visible and the page still works.
     2. Copy buttons. Reveals the per-snippet copy buttons and wires them to the
        clipboard, with an execCommand fallback and an aria-live confirmation.

   No framework, no build step: this ships as a plain static script that runs on
   GitHub Pages.
   ========================================================================== */
(function () {
  "use strict";

  function ready(fn) {
    if (document.readyState !== "loading") {
      fn();
    } else {
      document.addEventListener("DOMContentLoaded", fn);
    }
  }

  function legacyCopy(text) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "absolute";
    ta.style.left = "-9999px";
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try {
      ok = document.execCommand("copy");
    } catch (e) {
      ok = false;
    }
    document.body.removeChild(ta);
    return ok;
  }

  ready(function () {
    var pickers = document.querySelectorAll(".pp");
    if (!pickers.length) {
      return;
    }

    pickers.forEach(function (pp) {
      pp.classList.add("pp-js");

      var live = pp.querySelector(".pp-live");
      var announce = function (msg) {
        if (live) {
          live.textContent = "";
          // Re-set on the next frame so repeated messages re-announce.
          window.setTimeout(function () {
            live.textContent = msg;
          }, 30);
        }
      };

      /* --- Panel switching --------------------------------------------- */
      var radios = pp.querySelectorAll('input[type="radio"]');
      var showPanel = function (id) {
        pp.querySelectorAll(".pp-panel").forEach(function (panel) {
          panel.classList.toggle("pp-active", panel.id === id);
        });
      };
      var initial = null;
      radios.forEach(function (r) {
        if (r.checked) {
          initial = r;
        }
        r.addEventListener("change", function () {
          if (r.checked) {
            showPanel(r.getAttribute("data-panel"));
          }
        });
      });
      showPanel(initial ? initial.getAttribute("data-panel") : radios[0] && radios[0].getAttribute("data-panel"));

      /* --- Copy buttons ------------------------------------------------- */
      pp.querySelectorAll(".pp-copy").forEach(function (btn) {
        var label = btn.getAttribute("data-copy-label") || "snippet";
        btn.setAttribute("aria-label", "Copy " + label);
        btn.addEventListener("click", function () {
          // Find the snippet by DOM position, not id: Material rewrites <pre> ids
          // (prefixes them with "__code_"), so an id lookup would miss.
          var container = btn.closest(".pp-code");
          var target = container && container.querySelector("pre");
          if (!target) {
            return;
          }
          var text = target.innerText.replace(/\s+$/, "");
          var finish = function (ok) {
            btn.classList.toggle("pp-copied", ok);
            announce(ok ? "Copied " + label : "Could not copy " + label);
            window.clearTimeout(btn._resetTimer);
            btn._resetTimer = window.setTimeout(function () {
              btn.classList.remove("pp-copied");
            }, 2000);
          };
          if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text).then(
              function () { finish(true); },
              function () { finish(legacyCopy(text)); }
            );
          } else {
            finish(legacyCopy(text));
          }
        });
      });
    });
  });
})();
