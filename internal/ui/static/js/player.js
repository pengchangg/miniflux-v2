// Mini player and soft navigation.
//
// Pages are rendered on the server, so a regular navigation destroys the <audio> element and
// stops the playback. When an enclosure is playing we move its controls to a fixed bar at the
// bottom of the page and replace only the #page container on internal navigation, which keeps
// the media element alive.

// Simple Polyfill for browsers that don't support Trusted Types
// See https://caniuse.com/?search=trusted%20types
if (!window.trustedTypes || !trustedTypes.createPolicy) {
    window.trustedTypes = {
        createPolicy: (name, policy) => ({
            createScriptURL: src => src,
            createHTML: html => html,
        })
    };
}

// Policies are created lazily and cached: creating the same policy name twice throws an error.
let trustedHTMLPolicy = null;
let trustedURLPolicy = null;

// Set after the first page swap: the history entries can no longer be restored by the browser.
let softNavigationUsed = false;

/**
 * Create Trusted HTML content.
 *
 * @param {string} html - The HTML to trust.
 * @returns {TrustedHTML}
 */
function createTrustedHTML(html) {
    if (!trustedHTMLPolicy) {
        trustedHTMLPolicy = trustedTypes.createPolicy("html", {createHTML: value => value});
    }

    return trustedHTMLPolicy.createHTML(html);
}

/**
 * Create a Trusted Script URL.
 *
 * @param {string} url - The URL to trust.
 * @returns {TrustedScriptURL}
 */
function createTrustedScriptURL(url) {
    if (!trustedURLPolicy) {
        trustedURLPolicy = trustedTypes.createPolicy("url", {createScriptURL: value => value});
    }

    return trustedURLPolicy.createScriptURL(url);
}

const MINI_PLAYER_PLACEHOLDER_CLASS = "enclosure-audio-placeholder";

// Internal paths that must always be requested by the browser (downloads, POST only pages, assets).
const SOFT_NAVIGATION_EXCLUDED_PREFIXES = [
    "/api/",
    "/oauth2/",
    "/login",
    "/logout",
    "/export",
    "/import",
    "/js/",
    "/stylesheets/",
    "/icon/",
    "/manifest.json",
];

/**
 * @returns {Element|null} The mini player container.
 */
function getMiniPlayer() {
    return document.getElementById("mini-player");
}

/**
 * @returns {boolean} True when an enclosure is held by the mini player.
 */
function isMiniPlayerActive() {
    const miniPlayer = getMiniPlayer();
    return miniPlayer !== null && !miniPlayer.hidden;
}

/**
 * Build the element displayed in place of the enclosure controls moved to the mini player.
 *
 * @param {string} enclosureID - The enclosure ID.
 * @returns {Element}
 */
function createMiniPlayerPlaceholder(enclosureID) {
    const miniPlayer = getMiniPlayer();
    const placeholder = document.createElement("div");

    placeholder.className = MINI_PLAYER_PLACEHOLDER_CLASS;
    placeholder.dataset.enclosureId = enclosureID;
    placeholder.textContent = miniPlayer ? miniPlayer.dataset.labelPlaceholder : "";

    return placeholder;
}

/**
 * Move the enclosure controls of the playing audio element to the mini player.
 *
 * @param {Element} wrapper - The .enclosure-audio container of the media element.
 */
function moveMediaToMiniPlayer(wrapper) {
    const miniPlayer = getMiniPlayer();
    if (!wrapper || !miniPlayer) {
        return;
    }

    const slot = miniPlayer.querySelector(".mini-player-slot");
    if (wrapper.parentNode === slot) {
        return;
    }

    const mediaElement = wrapper.querySelector("audio[data-enclosure-id]");
    if (!mediaElement) {
        return;
    }

    // Only one enclosure is played at a time: stop the previous one and release its lock.
    const previousMediaElement = slot.querySelector("audio[data-playback-url]");
    if (previousMediaElement) {
        previousMediaElement.pause();
        releasePlayback(previousMediaElement);

        while (slot.firstChild) {
            slot.removeChild(slot.firstChild);
        }
    }

    const titleElement = document.querySelector("#page-header-title");
    miniPlayer.querySelector(".mini-player-title").textContent = titleElement ? titleElement.textContent : "";
    miniPlayer.dataset.enclosureId = mediaElement.dataset.enclosureId;

    wrapper.parentNode.replaceChild(createMiniPlayerPlaceholder(mediaElement.dataset.enclosureId), wrapper);
    slot.appendChild(wrapper);

    miniPlayer.hidden = false;
    document.body.classList.add("has-mini-player");
}

/**
 * Replace the enclosure controls of the playing episode with a placeholder.
 *
 * Called after each page rendering to avoid having two players for the same enclosure.
 */
function applyMiniPlayerPlaceholder() {
    const miniPlayer = getMiniPlayer();
    if (!miniPlayer || miniPlayer.hidden || !miniPlayer.dataset.enclosureId) {
        return;
    }

    const mediaElement = document.querySelector(`#page audio[data-enclosure-id="${miniPlayer.dataset.enclosureId}"]`);
    if (!mediaElement) {
        return;
    }

    const wrapper = mediaElement.closest(".enclosure-audio");
    if (wrapper) {
        wrapper.parentNode.replaceChild(createMiniPlayerPlaceholder(miniPlayer.dataset.enclosureId), wrapper);
    }
}

/**
 * Stop the playback, release the lock, and put the controls back in the page when possible.
 */
function closeMiniPlayer() {
    const miniPlayer = getMiniPlayer();
    if (!miniPlayer || miniPlayer.hidden) {
        return;
    }

    const mediaElement = miniPlayer.querySelector("audio[data-playback-url]");
    if (mediaElement) {
        mediaElement.pause();
        releasePlayback(mediaElement);
    }

    const wrapper = miniPlayer.querySelector(".mini-player-slot > *");
    const placeholder = document.querySelector(`#page .${MINI_PLAYER_PLACEHOLDER_CLASS}[data-enclosure-id="${miniPlayer.dataset.enclosureId}"]`);

    if (wrapper) {
        if (placeholder) {
            placeholder.parentNode.replaceChild(wrapper, placeholder);
        } else {
            wrapper.remove();
        }
    }

    miniPlayer.querySelector(".mini-player-title").textContent = "";
    miniPlayer.dataset.enclosureId = "";
    miniPlayer.hidden = true;
    document.body.classList.remove("has-mini-player");
}

/**
 * Navigate to an internal URL, keeping the playback alive when the mini player is active.
 *
 * @param {string} url - The target URL.
 */
function navigate(url) {
    if (!isMiniPlayerActive()) {
        window.location.href = url;
        return;
    }

    softNavigate(url, {});
}

/**
 * Check if a link can be loaded without a full page reload.
 *
 * @param {Element} link - The clicked link.
 * @returns {boolean}
 */
function isSoftNavigableLink(link) {
    if (!link || !link.href) {
        return false;
    }

    if (link.target && link.target !== "_self") {
        return false;
    }

    if (link.hasAttribute("download")) {
        return false;
    }

    if (link.rel && link.rel.split(/\s+/).includes("external")) {
        return false;
    }

    const url = new URL(link.href, window.location.href);

    if (url.origin !== window.location.origin) {
        return false;
    }

    // In-page anchor: let the browser scroll.
    if (url.hash && url.pathname === window.location.pathname && url.search === window.location.search) {
        return false;
    }

    return !SOFT_NAVIGATION_EXCLUDED_PREFIXES.some((prefix) => url.pathname.startsWith(prefix));
}

/**
 * Update the highlighted menu entry after a page swap.
 *
 * @param {string} menu - The current menu of the new page.
 */
function updateCurrentMenu(menu) {
    if (!menu) {
        return;
    }

    document.body.dataset.currentMenu = menu;

    document.querySelectorAll("#header-menu li").forEach((item) => {
        const link = item.querySelector("a[data-page]");
        item.classList.toggle("active", link !== null && link.dataset.page === menu);
    });
}

/**
 * Replace the content of the current page with the content of the given URL.
 *
 * Falls back to a regular navigation when the response cannot be used.
 *
 * @param {string} url - The target URL.
 * @param {Object} options - Options.
 * @param {boolean} [options.replace] - Replace the current history entry instead of pushing a new one.
 * @param {number} [options.scrollY] - Scroll position to restore.
 */
function softNavigate(url, options) {
    history.replaceState({scrollY: window.scrollY}, "");

    fetch(url, {credentials: "same-origin", headers: {Accept: "text/html"}})
        .then((response) => {
            const contentType = response.headers.get("Content-Type") || "";

            // Redirected responses (expired session for instance) are handled by the browser.
            if (!response.ok || response.redirected || !contentType.includes("text/html")) {
                window.location.href = response.url || url;
                return null;
            }

            return response.text();
        })
        .then((html) => {
            if (html === null) {
                return;
            }

            // DOMParser.parseFromString("text/html") is a Trusted Types sink, so it needs the policy too.
            const newDocument = new DOMParser().parseFromString(createTrustedHTML(html), "text/html");
            const newPage = newDocument.getElementById("page");
            if (!newPage) {
                window.location.href = url;
                return;
            }

            document.getElementById("page").innerHTML = createTrustedHTML(newPage.innerHTML);
            document.title = newDocument.title;
            updateCurrentMenu(newDocument.body.dataset.currentMenu);

            if (options.replace) {
                history.replaceState({scrollY: options.scrollY || 0}, "", url);
            } else {
                history.pushState({scrollY: 0}, "", url);
            }

            window.scrollTo(0, options.scrollY || 0);
            softNavigationUsed = true;
            initializePageHandlers();
        })
        .catch(() => {
            window.location.href = url;
        });
}

/**
 * Initialize the mini player controls.
 */
function initializeMiniPlayer() {
    const miniPlayer = getMiniPlayer();
    if (!miniPlayer) {
        return;
    }

    miniPlayer.querySelector(".mini-player-close").addEventListener("click", closeMiniPlayer);
}

/**
 * Install the handlers that must exist once per document.
 */
function installNavigationInterceptors() {
    document.addEventListener("click", (event) => {
        if (event.defaultPrevented || event.button !== 0 || event.ctrlKey || event.shiftKey || event.metaKey || event.altKey) {
            return;
        }

        if (!isMiniPlayerActive()) {
            return;
        }

        const link = event.target.closest("a");
        if (!isSoftNavigableLink(link)) {
            return;
        }

        event.preventDefault();
        navigate(link.href);
    });

    window.addEventListener("popstate", (event) => {
        if (!softNavigationUsed) {
            return;
        }

        if (!isMiniPlayerActive()) {
            // The document was replaced by a page swap, the browser cache has nothing to restore.
            window.location.reload();
            return;
        }

        const scrollY = event.state && typeof event.state.scrollY === "number" ? event.state.scrollY : 0;
        softNavigate(window.location.href, {replace: true, scrollY: scrollY});
    });

    // Browsers only fire pagehide on real navigations, but the media element may have been moved.
    window.addEventListener("pagehide", () => {
        document.querySelectorAll("audio[data-playback-url],video[data-playback-url]").forEach((element) => {
            releasePlaybackOnPageHide(element);
        });
    });

    // Remote pause commands are delivered through the same interval as the lock state updates.
    window.setInterval(pollPlaybackState, 10 * 1000);
}
