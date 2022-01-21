var textMaxLength = ""; // Previous value of maxlength of #text
var textRows = ""; // Previous value of rows of #text

var $mentionedList = u("#mentioned-list").first(); // node list of mentioned users
var lastSymbol = ""; // last char in textarea

// Array.findIndex polyfill
if (!Array.prototype.findIndex) {
  Array.prototype.findIndex = function(predicate) {
    if (this == null) {
      throw new TypeError(
        "Array.prototype.findIndex called on null or undefined"
      );
    }
    if (typeof predicate !== "function") {
      throw new TypeError("predicate must be a function");
    }
    var list = Object(this);
    var length = list.length >>> 0;
    var thisArg = arguments[1];
    var value;

    for (var i = 0; i < length; i++) {
      value = list[i];
      if (predicate.call(thisArg, value, i, list)) {
        return i;
      }
    }
    return -1;
  };
}

if (!Array.prototype.find) {
  Object.defineProperty(Array.prototype, "find", {
    value: function(predicate) {
      // 1. Let O be ? ToObject(this value).
      if (this == null) {
        throw new TypeError('"this" is null or not defined');
      }

      var o = Object(this);

      // 2. Let len be ? ToLength(? Get(O, "length")).
      var len = o.length >>> 0;

      // 3. If IsCallable(predicate) is false, throw a TypeError exception.
      if (typeof predicate !== "function") {
        throw new TypeError("predicate must be a function");
      }

      // 4. If thisArg was supplied, let T be thisArg; else let T be undefined.
      var thisArg = arguments[1];

      // 5. Let k be 0.
      var k = 0;

      // 6. Repeat, while k < len
      while (k < len) {
        // a. Let Pk be ! ToString(k).
        // b. Let kValue be ? Get(O, Pk).
        // c. Let testResult be ToBoolean(? Call(predicate, T, « kValue, k, O »)).
        // d. If testResult is true, return kValue.
        var kValue = o[k];
        if (predicate.call(thisArg, kValue, k, o)) {
          return kValue;
        }
        // e. Increase k by 1.
        k++;
      }

      // 7. Return undefined.
      return undefined;
    },
    configurable: true,
    writable: true,
  });
}

function replyTo(e) {
  e.preventDefault();

  movePostBox(e);

  var el = u("textarea#text");
  var text = u("#text").first();

  var reply = u(e.target).data("reply");

  clearMentionedList();

  if (u("#replyTo").first().value != "") {
    reply = reply.replace("(" + u("#replyTo").first().value + ") ", "")
  }

  el.empty();

  text.value = reply;
  text.focus();

  var size = text.value.length;
  text.setSelectionRange(size, size);
}

function forkFrom(e) {
  e.preventDefault();

  movePostBox(e);

  var el = u("textarea#text");
  var text = u("#text").first();

  el.empty();

  text.value = u(e.target).data("fork");
  text.focus();

  var size = text.value.length;
  text.setSelectionRange(size, size);
}

function editTwt(e) {
  e.preventDefault();

  movePostBox(e);

  var el = u("textarea#text");
  var text = u("#text").first();

  el.empty();

  text.value = u(e.target).data("text");
  text.focus();

  var size = text.value.length;
  text.setSelectionRange(size, size);

  u("#replaceTwt").first().value = u(e.target).data("hash");
}

function deleteTwt(e) {
  e.preventDefault();

  if (
    confirm("Are you sure you want to delete this twt? This cannot be undone!")
  ) {
    Twix.ajax({
      type: "DELETE",
      url: u("#form").attr("action"),
      data: new FormData(u("#form").first()),
      success: function(data) {
        var hash = u(e.target).data("hash");
        u("#" + hash).remove();
      },
    });
  }
}

function movePostBox(e) {
  e.preventDefault();

  u("article").each(function(n, i){
    u(n).removeClass("highlight");
  });

  var article = u(e.target).closest(".twt-nav").parent();
  var postbox = u("#postbox").clone();

  article.addClass("highlight");

  u("#postbox").remove();
  article.after(postbox);
  postbox.addClass("drawer");

  u("#toolbar").addClass("toolbar-reply");
  u("#form").addClass("form-reply");

  u(".grid.h-feed").addClass("bump-up");
  article.scroll();
}

/* Close the PostBox on Escape if we moved it */
u("body").on("keyup", function(e) {
  if (u("#postbox").hasClass("drawer")) {
    if (e.key === "Escape") {
      e.preventDefault();
      resetPostBox();
      u("#text").first().value = "";
    }
  }
});

function resetPostBox() {
  if (!u("#form").hasClass("form-reply")) {
    return;
  }

  u('article').each(function(n){
    u(n).removeClass('highlight');
  });

  var postbox = u("#postbox").clone();
  postbox.removeClass("drawer");

  u("#postbox").remove();
  u("main").prepend(postbox);

  u("#toolbar").removeClass("toolbar-reply");
  u("#form").removeClass("form-reply");

  u('.grid.h-feed').removeClass("bump-up");
  u("main").scroll();
}

u("#theme select").on("change", function(e) {
  var value = u(e.target).first().value;

  switch (value) {
    case "auto":
      u("html").data("theme", "");
      break;
    case "light":
    case "dark":
    case "light-classic":
    case "dark-classic":
      u("html").data("theme", value);
      break;
    default:
      console.error("invalid theme: " + value);
  }
});

function persist(e) {
  localStorage.setItem(e.target.id, e.target.value);
}

u("input#title").on("change", persist);
u("textarea#text").on("change", persist);

u(".replyBtn").on("click", replyTo);
u(".forkBtn").on("click", forkFrom);
u(".editBtn").on("click", editTwt);
u(".deleteBtn").on("click", deleteTwt);

u("#post").on("click", function(e) {
  e.preventDefault();
  
  var form = u("#form").first();

  if (!form.checkValidity()) {
    form.reportValidity();
    return;
  }

  localStorage.setItem('title', '');
  localStorage.setItem('text', '');
  u("#post").html('Posting...');
  u("#post").attr("aria-busy", true);
  u("#post").attr("disabled", true);
  form.submit();
});

u(".bookmarkBtn").on("click", function (e) {
  e.preventDefault();
  Twix.ajax({
    type: "GET",
    url: u(e.target).closest("a.bookmarkBtn").attr("href"),
    success: function(data) {
      u(e.target).closest("a.bookmarkBtn").attr("style", "display: none;");
      u(e.target).parent().parent().find("a.unbookmarkBtn").attr("style", "display: inline;");
    },
  });
});

u(".unbookmarkBtn").on("click", function (e) {
  e.preventDefault();

  Twix.ajax({
    type: "GET",
    url: u(e.target).closest("a.unbookmarkBtn").attr("href"),
    success: function(data) {
      u(e.target).closest("a.unbookmarkBtn").attr("style", "display: none;");
      u(e.target).parent().parent().find("a.bookmarkBtn").attr("style", "display: inline;");
    },
  });
});

u(".followBtn").on("click", function (e) {
  e.preventDefault();

  Twix.ajax({
    type: "GET",
    url: u(e.target).closest("a.followBtn").attr("href"),
    success: function(data) {
      u(e.target).closest("a.followBtn").attr("style", "display: none;");
      u(e.target).parent().find("a.unfollowBtn").attr("style", "display: inline;");
    },
  });
});

u(".unfollowBtn").on("click", function (e) {
  e.preventDefault();

  Twix.ajax({
    type: "GET",
    url: u(e.target).closest("a.unfollowBtn").attr("href"),
    success: function(data) {
      u(e.target).closest("a.unfollowBtn").attr("style", "display: none;");
      u(e.target).parent().find("a.followBtn").attr("style", "display: inline;");
    },
  });
});

u("#muteBtn").on("click", function (e) {
  e.preventDefault();

  Twix.ajax({
    type: "GET",
    url: u(e.target).attr("href"),
    success: function(data) {
      u(e.target).attr("style", "display: none;");
      u("#unmuteBtn").attr("style", "display: inline;");
    },
  });
});

u("#unmuteBtn").on("click", function (e) {
  e.preventDefault();

  Twix.ajax({
    type: "GET",
    url: u(e.target).attr("href"),
    success: function(data) {
      u(e.target).attr("style", "display: none;");
      u("#muteBtn").attr("style", "display: inline;");
    },
  });
});

u.prototype.isHidden = function () {
  var e = this.first();
  return (e.offsetParent === null)
};

u.prototype.getSelection = function() {
  var e = this.first();

  return (
    /* mozilla / dom 3.0 */
    (
      ("selectionStart" in e &&
        function() {
          var l = e.selectionEnd - e.selectionStart;
          return {
            start: e.selectionStart,
            end: e.selectionEnd,
            length: l,
            text: e.value.substr(e.selectionStart, l),
          };
        }) ||
      /* exploder */
      (document.selection &&
        function() {
          e.focus();

          var r = document.selection.createRange();
          if (r === null) {
            return {
              start: 0,
              end: e.value.length,
              length: 0
            };
          }

          var re = e.createTextRange();
          var rc = re.duplicate();
          re.moveToBookmark(r.getBookmark());
          rc.setEndPoint("EndToStart", re);

          return {
            start: rc.text.length,
            end: rc.text.length + r.text.length,
            length: r.text.length,
            text: r.text,
          };
        }) ||
      /* browser not supported */
      function() {
        return null;
      }
    )()
  );
};

u.prototype.replaceSelection = function() {
  var e = this.first();

  var text = arguments[0] || "";

  return (
    /* mozilla / dom 3.0 */
    (
      ("selectionStart" in e &&
        function() {
          e.value =
            e.value.substr(0, e.selectionStart) +
            text +
            e.value.substr(e.selectionEnd, e.value.length);
          return this;
        }) ||
      /* exploder */
      (document.selection &&
        function() {
          e.focus();
          document.selection.createRange().text = text;
          return this;
        }) ||
      /* browser not supported */
      function() {
        e.value += text;
        return jQuery(e);
      }
    )()
  );
};

function createMentionedUserNode(match) {
  return u("<div>")
    .addClass("user-list__user")
    .append(
      u("<div>")
      .addClass("avatar")
      .attr(
        "style",
        "background-image: url('" + match.Avatar + "')"
      )
    )
    .append(
      u("<div>")
      .addClass("info")
      .append(u("<div>").addClass("nickname").text(match.Nick))
    );
}

function formatText(selector, fmt) {
  selector.first().focus();

  var finalText = "";
  var start = selector.first().selectionStart;
  var selectedText = selector.getSelection().text;

  if (fmt.includes("(url)")) {
    if (selectedText.length == 0) {
      finalText = fmt;
    } else {
      finalText = fmt.replace("url", selectedText);
    }
  } else {
    if (selectedText.length == 0) {
      finalText = fmt + fmt;
    } else {
      finalText = fmt + selectedText + fmt;
    }
  }

  selector.replaceSelection(finalText, true);
  selector.first().focus();
  if (!selectedText.length) {
    var selectionRange = start + fmt.length;
    selector.first().setSelectionRange(selectionRange, selectionRange);
  }
}

function insertText(selector, text) {
  var start = selector.first().selectionStart;

  selector.first().value.slice(startMention, start);
  selector.replaceSelection(text, false);
  selector.first().focus();

  var selectionRange =
    selector.first().value.substr(start + text.length - 1, 1) === ")" ?
    start + text.length - 1 :
    start + text.length;

  selector.first().setSelectionRange(selectionRange, selectionRange);
}

function iOS() {
  return (
    [
      "iPad Simulator",
      "iPhone Simulator",
      "iPod Simulator",
      "iPad",
      "iPhone",
      "iPod",
    ].indexOf(navigator.platform) !== -1 ||
    // iPad on iOS 13 detection
    (navigator.userAgent.indexOf("Mac") !== -1 && "ontouchend" in document)
  );
}

function IE() {
  return !!window.MSInputMethodContext && !!document.documentMode;
}

var deBounce = 300;
var fetchUsersTimeout = null;

function getUsers(searchStr) {
  clearTimeout(fetchUsersTimeout);
  fetchUsersTimeout = setTimeout(function() {
    let requestUrl = "/lookup";

    if (searchStr) {
      requestUrl += "?prefix=" + searchStr;
    }

    Twix.ajax({
      type: "GET",
      url: requestUrl,
      success: function(data) {
        u("#mentioned-list-content").empty();
        data.map(function(match) {
          u("#mentioned-list-content").append(createMentionedUserNode(match));
        });
        if (data.length) {
          u(".user-list__user").first().classList.add("selected");
        }
      },
    });
  }, deBounce);
}

var mentions = [];

u(".img-orig-open").on("click", function(e) {
  e.preventDefault();
  toggleModal(e);
});

u("dialog img").on("click", function(e) {
  e.preventDefault();
  if (visibleModal != null) {
    closeModal(visibleModal);
  }
});

u("#bBtn").on("click", function(e) {
  e.preventDefault();
  formatText(u("textarea#text"), "**");
});

u("#iBtn").on("click", function(e) {
  e.preventDefault();
  formatText(u("textarea#text"), "_");
});

u("#sBtn").on("click", function(e) {
  e.preventDefault();
  formatText(u("textarea#text"), "~~");
});

u("#cBtn").on("click", function(e) {
  e.preventDefault();
  formatText(u("textarea#text"), "`");
});

u("#lnkBtn").on("click", function(e) {
  e.preventDefault();
  formatText(u("textarea#text"), "[title](url)");
});

u("#imgBtn").on("click", function(e) {
  e.preventDefault();
  formatText(u("textarea#text"), "![](url)");
});

u("#usrBtn").on("click", function(e) {
  e.preventDefault();
  if (!u("#mentioned-list").isHidden()) {
    u("textarea#text").first().focus();
    startMention = u("textarea#text").first().selectionStart + 1;
    insertText(u("textarea#text"), "@");
    if (iOS() || IE()) {
      showMentionedList();
      getUsers();
    }
  } else {
    clearMentionedList();
  }
});

u("textarea#text").on("keydown", function(e) {
  if (e.ctrlKey && e.keyCode == 13) {
    e.preventDefault();
    u("#post").trigger("click");
  }
});

u("textarea#text").on("focus", function(e) {
  if (e.relatedTarget === u("#usrBtn").first()) {
    showMentionedList();
    getUsers();
  }
});

var startMention = null;

u("textarea#text").on("keyup", function(e) {
  if (e.key.length === 1 || e.key === "Backspace") {
    var idx = e.target.selectionStart;
    var prevSymbol = e.target.value.slice(idx - 1, idx);

    if (prevSymbol === "@") {
      startMention = idx;
      showMentionedList();
    }

    if (!u("#mentioned-list").isHidden()) {
      var searchStr = e.target.value.slice(startMention, idx);
      if (!prevSymbol.trim()) {
        clearMentionedList();
        startMention = null;
      } else {
        getUsers(searchStr);
      }
    }
  }
});

u("#mentioned-list-content").on("mousemove", function(e) {
  var target = e.target;
  u(".user-list__user").nodes.forEach(function(item) {
    item.classList.remove("selected");
  });
  if (target.classList.contains("user-list__user")) {
    target.classList.add("selected");
  }
});

u("#mentioned-list").on("click", function(e) {
  var value = u("textarea#text").first().value;

  u("textarea#text").first().value =
    value.slice(0, startMention) +
    value.slice(u("textarea#text").first().selectionEnd);

  u("textarea#text").first().setSelectionRange(startMention, startMention);
  insertText(u("textarea#text"), e.target.innerText.trim());
  u("#mentioned-list").attr("style", "display: none;");
});

var maxTaskWait = (1000 * 60 * 10); // ~10mins TODO: Make this configurable

function pollForTask(taskURL, delay, maxDelay, timeout, errorCallback, successCallback) {
  Twix.ajax({
    type: "GET",
    url: taskURL,
    error: function(statusCode, statusText) {
      errorCallback({
        error: statusCode + " " + statusText
      })
    },
    success: function(data) {
      switch (data.state) {
        case "pending":
        case "running":
          if (Date.now() < timeout) {
            if (delay < maxDelay) {
              delay = delay * 2;
            }
            setTimeout(function() {
              pollForTask(taskURL, delay, maxDelay, timeout, errorCallback, successCallback);
            }, delay);
            return;
          }
          break;
        case "complete":
          successCallback(data);
          break;
        default:
          errorCallback(data);
      }
    },
  });
}

u("#uploadMedia").on("change", function(e) {
  u("#uploadMediaButton").attr("aria-busy",true);
  u("#uploadMediaForm").data("tooltip", "Uploading...");

  Twix.ajax({
    type: "POST",
    url: u("#mediaUploadForm").attr("action"),
    data: new FormData(u("#mediaUploadForm").first()),
    success: function(data) {
      var el = u("textarea#text");
      var text = document.getElementById("text");

      pollForTask(
        data.Path,
        1000,
        30000,
        Date.now() + maxTaskWait,

        function(errorData) {
          u("#uploadMediaButton").attr("aria-busy",false);
          alert("An error occurred uploading your media: " + errorData.error)
        },

        function(successData) {
          text.value += " ![](" + successData.data.mediaURI + ") ";
          el.scroll();
          text.focus();

          var size = el.text().length;
          text.setSelectionRange(size, size);

          u("#uploadMediaButton").attr("aria-busy",false);
          u("#uploadMedia").data("tooltip", "Upload");
        }

      );
    },

    error: function(statusCode, statusText) {
      u("#uploadMediaButton").attr("aria-busy",false);
      alert("An error occurred uploading your media: " + statusCode + " " + statusText);
    },

  });

});

u("#register > button").first().disabled = true;
u("#register #agree").on("change", function(e) {
  if (u(e.target).first().checked) {
    u("#register > button").first().disabled = false;
  } else {
    u("#register > button").first().disabled = true;
  }
});

u("#burgerMenu").on("click", function(e) {
  e.preventDefault();
  if (u("#podMenu").first().style.display === "none" ||
  !(u("#podMenu").first().hasAttribute("style"))) {
    u("#podMenu").first().style.display = "grid";
  } else {
    u("#podMenu").first().style.display = "none";
  }
});

u("body").on("keydown", function(e) {
  if (u("#mentioned-list").isHidden()) {
    return;
  }

  if (e.key === "Escape") {
    clearMentionedList();
  }

  if (
    e.key === "ArrowUp" ||
    e.key === "ArrowDown" ||
    e.key === "Up" ||
      e.key === "Down"
    ) {
      e.preventDefault();

      var selectedIdx = u(".user-list__user").nodes.findIndex(function(
        item
      ) {
        return item.classList.contains("selected");
      });

      var nextIdx;
      var scrollOffset;

      if (e.key === "ArrowDown" || e.key === "Down") {
        nextIdx =
          selectedIdx + 1 === u(".user-list__user").length ?
          0 :
          selectedIdx + 1;
      } else if (e.key === "ArrowUp" || e.key === "Up") {
        nextIdx =
          selectedIdx - 1 < 0 ?
          u(".user-list__user").length - 1 :
          selectedIdx - 1;
      }

      scrollOffset =
        u(".user-list__user").first().clientHeight * (nextIdx - 2);

      u(".user-list__user").nodes.forEach(function(item, index) {
        item.classList.remove("selected");
        if (index === nextIdx) {
          u("#mentioned-list-content").first().scrollTop =
            scrollOffset > 0 ? scrollOffset : 0;
          item.classList.add("selected");
        }
      });
    }

    if (e.key === "Tab" || e.key === "Enter") {
      e.preventDefault();

      var selectedNodeIdx = u(".user-list__user").nodes.findIndex(function(
        item
      ) {
        return item.classList.contains("selected");
      });

      var selectedNode = u(".user-list__user").nodes[selectedNodeIdx];

      var value = u("textarea#text").first().value;

      u("textarea#text").first().value =
        value.slice(0, startMention) +
        value.slice(u("textarea#text").first().selectionEnd);

      u("textarea#text")
        .first()
        .setSelectionRange(startMention, startMention);
      insertText(u("textarea#text"), selectedNode.innerText.trim());
      clearMentionedList();
    }

    var caret = u("textarea#text").first().selectionStart;
    var prevSymbol = u("textarea#text")
      .first()
      .value.slice(caret - 1, 1);

    if (e.key === "Backspace" && prevSymbol === "@") {
      clearMentionedList();
    }
});

function clearMentionedList() {
  u("#mentioned-list").attr("style", "display: none;");
  u("#mentioned-list-content").first().innerHTML = "";
}

function showMentionedList() {
  u("#mentioned-list").attr("style", "display: inherit;");
  u("#mentioned-list").first().style.top =
    u("textarea#text").first().clientHeight + 2 + "px";
}

if (
  window.performance.getEntriesByType("navigation")[0].type === "back_forward"
) {
  window.scrollTo(0, Number(localStorage.getItem("prevOffset")));
}

window.onbeforeunload = function() {
  localStorage.setItem(
    "prevOffset",
    localStorage.getItem("currentOffset") || String(window.scrollY)
  );
  localStorage.setItem("currentOffset", String(window.scrollY));
};

window.onload = function() {
  var text = localStorage.getItem('text');
  if (text) {
    u("textarea#text").first().value = text;
    return;
  }

  // Support Bookmarklet
  /*
    var url = document.URL ;
    var title = document.title ;

    window.location.href = "https://twtxt.net"
                            + "/?title="
                            + title + "&url="
                            + url;
  */
  if (typeof(window.URLSearchParams) != "undefined") {
    const urlParams = new URLSearchParams(window.location.search);
    const titleParam = urlParams.get("title");
    const urlParam = urlParams.get("url");
    if (titleParam && urlParam) {
      insertText(u("textarea#text"), "[" + titleParam + "](" + urlParam + ")\r\n\r\n");
    }
  }
}
