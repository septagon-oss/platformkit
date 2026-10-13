// The rich text field's browser half. It mounts the vendored editor on every
// [data-richtext] field the page drew, keeps the field's own <textarea> as the
// submitted value, binds behaviour to the chrome ui/forms drew, and saves.
//
// It touches three URLs and no others: the upload route, the record's own address,
// and the address a stored picture's bytes are read back from, all three written by
// the server. It stores one key and no others: the draft key the server composed.
// It reads no cookie and invents no path — an attribute the page left out is a
// feature that is off, not one to guess at.
//
// With this file absent the page is the same page: the textarea posts, the server
// validates every line, and "Formatting help" is the guidance. The editor is an
// enhancement, and the form is the contract.
import {
	Blockquote, Bold, BulletList, CharacterCount, Code, CodeBlock, Document,
	HardBreak, Heading, History, HorizontalRule, Image, Italic, Link, ListItem,
	Markdown, OrderedList, Paragraph, Placeholder, Strike, TableKit, TaskItem,
	TaskList, Text, Editor,
} from "../richtext/editor.js";

const AUTOSAVE_AFTER = 2000;
const DOCUMENT_SHAPE = 1;
const LINK_SCHEMES = ["https:", "http:", "mailto:", "tel:"];

// mount turns one drawn field into one editor. A field the server marked read-only
// or disabled carries no chrome to bind, and gets nothing: the textarea stays
// visible and posts as it always did.
function mount(root) {
	const field = root.dataset.richtextField;
	const textarea = root.querySelector("textarea[name='" + field + "']");
	const surface = root.querySelector("[data-richtext-surface]");
	if (!textarea || !surface) return;

	const control = {
		root, textarea, surface, field,
		toolbar: root.querySelector("[data-richtext-toolbar]"),
		more: root.querySelector("[data-richtext-more]"),
		morePanel: root.querySelector("[data-richtext-more-panel]"),
		bubble: root.querySelector("[data-richtext-bubble]"),
		menu: root.querySelector("[data-richtext-menu]"),
		link: root.querySelector("[data-richtext-link]"),
		linkInput: root.querySelector("[data-richtext-link-input]"),
		status: root.querySelector("[data-richtext-status]"),
		count: root.querySelector("[data-richtext-count]"),
		file: root.querySelector("[data-richtext-file]"),
		draftBar: root.querySelector("[data-richtext-draft-bar]"),
		preview: root.querySelector("[data-richtext-preview]"),
		offer: root.querySelector("[data-richtext-offer]"),
	};
	control.editor = createEditor(control);
	bindChrome(control);
	bindUploads(control);
	bindPersistence(control);
	reveal(control);
}

// createEditor builds one editor per field, with decision 0069 §1's schema and the
// field's own limits. The document is loaded and stored as Markdown: the format is
// the stored value, so the editor holds no second opinion about it.
function createEditor(control) {
	const limit = Number(control.root.dataset.richtextMax || 0);
	const extensions = [
		Document, Paragraph, Text,
		// Levels 2 to 4: kit/richtext refuses a level-1 heading at the write, so a
		// document holding one could be typed and never saved.
		Heading.configure({ levels: [2, 3, 4] }),
		Bold, Italic, Strike, Code, Link,
		Blockquote, CodeBlock, HardBreak, HorizontalRule,
		BulletList, OrderedList, ListItem, TaskList, TaskItem.configure({ nested: true }),
		TableKit,
		picture(control),
		History,
		Markdown.configure({ indentStyle: "space", indentSize: 2, renderer: { tightTypes: [] } }),
		Placeholder.configure({ showOnlyWhenEditable: false }),
	];
	if (limit > 0) extensions.push(CharacterCount.configure({ limit }));

	// Every callback below takes the one props object this Tiptap emits for its
	// event: {editor, transaction, appendedTransactions} for transaction and
	// update, {editor, transaction} for selectionUpdate, {editor, event, slice}
	// for paste. Keyboard handling is not an event here at all — the editor ships
	// no keydown event — so it is a ProseMirror editor prop.
	const editor = new Editor({
		element: control.surface,
		extensions,
		content: control.textarea.value || "",
		contentType: "markdown",
		editable: !control.textarea.disabled && !control.textarea.readOnly,
		editorProps: {
			handleKeyDown: (_view, event) => handleKey(control, event),
			// The editable element is the field's control: it carries the field's own
			// name, which the hidden textarea no longer speaks for.
			attributes: { "aria-label": control.root.dataset.richtextName || "", "aria-multiline": "true" },
		},
		onUpdate: () => commit(control),
		onSelectionUpdate: () => { positionBubble(control); updatePressed(control); },
		onTransaction: ({ transaction }) => { if (!transaction.docChanged) updatePressed(control); },
	});
	return editor;
}

// commit is the only writer of the textarea, and it writes on every document
// change: a submit that arrives between two keystrokes must not post a stale
// value. The change event is the form's own signal to whatever else is listening.
function commit(control) {
	const markdown = control.editor.getMarkdown();
	if (control.textarea.value !== markdown) {
		control.textarea.value = markdown;
		control.textarea.dispatchEvent(new Event("input", { bubbles: true }));
		control.textarea.dispatchEvent(new Event("change", { bubbles: true }));
	}
	count(control);
	mark(control, "dirty");
	control.save = schedule(control, control.save);
}

function count(control) {
	if (!control.count) return;
	const characters = control.textarea.value.length;
	const limit = Number(control.root.dataset.richtextMax || 0);
	const words = control.textarea.value.trim() === "" ? 0 : control.textarea.value.trim().split(/\s+/).length;
	control.count.textContent = limit > 0
		? wordsLabel(control, words) + " · " + characters + " / " + limit
		: wordsLabel(control, words);
	control.count.hidden = false;
	if (limit > 0 && characters > limit) mark(control, "over-limit");
}

function wordsLabel(control, words) {
	return word(control, "richtextWords").replace("{count}", String(words));
}

// word is one sentence the page already drew, in the request's language. This file
// holds no prose: a line the server did not draw is a line the field does not say,
// and never one the browser writes in whichever language it happens to ship in.
function word(control, name) {
	return control.root.dataset[name] || "";
}

// mark is how the rest of the page learns what the field is doing. A module's own
// screen can read the event and say something in its own words; the status line
// beside the field is the kernel's sentence about the same fact.
function mark(control, state, detail) {
	const payload = Object.assign({ state: state, at: Date.now() }, detail || {});
	control.root.dispatchEvent(new CustomEvent("richtext:status", { bubbles: true, detail: payload }));
	if (!control.status) return;
	const said = word(control, "richtextWord" + state.charAt(0).toUpperCase() + state.slice(1));
	if (said) {
		control.status.textContent = said;
		control.status.hidden = false;
	}
	control.status.dataset.state = state;
}

function schedule(control, previous) {
	if (previous) clearTimeout(previous);
	return setTimeout(() => save(control), AUTOSAVE_AFTER);
}

// reveal un-hides the chrome only once an editor actually stands behind it. A page
// whose module script never ran — no JavaScript, a fetch that failed, a browser
// too old for ES modules — keeps the plain textarea it was drawn with.
function reveal(control) {
	// One surface, not two. The textarea keeps the value and posts it, but the
	// editor is what the person sees and types into from here on: leaving both
	// visible would offer two documents for one field, and the editor's own wins
	// every commit, so the one a person could still type in would be a lie.
	control.textarea.hidden = true;
	if (control.toolbar) control.toolbar.hidden = false;
	control.surface.hidden = false;
	if (control.count) count(control);
	mark(control, "ready");
}

// bindChrome connects the drawn buttons to editor commands. Every control already
// exists, already carries its label in the page's language and already names its
// shortcut in its accessible name; this adds behaviour and the two states a toggle
// reports.
function bindChrome(control) {
	for (const button of control.root.querySelectorAll("[data-richtext-command]")) {
		button.addEventListener("click", (event) => {
			event.preventDefault();
			run(control, button.dataset.richtextCommand);
		});
	}
	if (control.more && control.morePanel) {
		control.more.addEventListener("click", () => {
			const open = control.morePanel.hidden;
			control.morePanel.hidden = !open;
			control.more.setAttribute("aria-expanded", String(open));
		});
	}
	bindSlash(control);
	bindLink(control);
	bindSelectionChrome(control);
	updatePressed(control);
}

// run is the command vocabulary. The server draws a name only when it means it,
// so a name with no case here is a bug rather than a fallback.
function run(control, command, argument) {
	const editor = control.editor;
	const chain = () => editor.chain().focus();
	switch (command) {
		case "bold": chain().toggleBold().run(); break;
		case "italic": chain().toggleItalic().run(); break;
		case "strike": chain().toggleStrike().run(); break;
		case "code": chain().toggleCode().run(); break;
		case "quote": chain().toggleBlockquote().run(); break;
		case "codeBlock": chain().toggleCodeBlock().run(); break;
		case "bulletList": chain().toggleBulletList().run(); break;
		case "orderedList": chain().toggleOrderedList().run(); break;
		case "taskList": chain().toggleTaskList().run(); break;
		case "table": chain().insertTable({ rows: 2, cols: 2, withHeaderRow: true }).run(); break;
		case "rule": chain().setHorizontalRule().run(); break;
		case "heading2": chain().toggleHeading({ level: 2 }).run(); break;
		case "heading3": chain().toggleHeading({ level: 3 }).run(); break;
		case "paragraph": chain().setParagraph().run(); break;
		case "undo": editor.commands.undo(); break;
		case "redo": editor.commands.redo(); break;
		case "link": openLink(control); break;
		case "link-save": saveLink(control); break;
		case "link-remove": removeLink(control); hideLink(control); break;
		case "image": pickFile(control); break;
		case "preview": togglePreview(control); break;
		default: mark(control, "error", { reason: "unknown command " + command });
	}
	if (argument) closeMenu(control);
	updatePressed(control);
}

function updatePressed(control) {
	const editor = control.editor;
	for (const button of control.root.querySelectorAll("[data-richtext-command]")) {
		const name = button.dataset.richtextCommand;
		if (!button.hasAttribute("aria-pressed")) continue;
		button.setAttribute("aria-pressed", String(isActive(control, name)));
	}
	void editor;
}

function isActive(control, name) {
	const editor = control.editor;
	const levels = { heading2: 2, heading3: 3 };
	if (name in levels) return editor.isActive("heading", { level: levels[name] });
	if (name === "paragraph") return editor.isActive("paragraph");
	if (name === "link") return editor.isActive("link");
	if (name === "quote") return editor.isActive("blockquote");
	if (name === "codeBlock") return editor.isActive("codeBlock");
	if (name === "bulletList") return editor.isActive("bulletList");
	if (name === "orderedList") return editor.isActive("orderedList");
	if (name === "taskList") return editor.isActive("taskList");
	if (name === "preview") return !editor.isEditable;
	return editor.isActive(name);
}

// positionBubble anchors the selection bubble to the selection itself, which is the
// one thing the browser can compute and the server cannot. No selection, no
// bubble: an empty floating toolbar is a widget asking to be ignored. The selection
// has to be this field's own as well — two fields share one page, and a range read
// somewhere else in it is not a call to move this field's toolbar.
function positionBubble(control) {
	if (!control.bubble) return;
	const selection = window.getSelection();
	if (!control.editor.isEditable || !selection || selection.isCollapsed || selection.rangeCount === 0
		|| !control.surface.contains(selection.getRangeAt(0).commonAncestorContainer)) {
		control.bubble.hidden = true;
		return;
	}
	const rect = selection.getRangeAt(0).getBoundingClientRect();
	if (rect.width === 0 && rect.height === 0) { control.bubble.hidden = true; return; }
	control.bubble.hidden = false;
	const box = control.bubble.getBoundingClientRect();
	const left = Math.max(8, rect.left + window.scrollX + rect.width / 2 - box.width / 2);
	const above = rect.top + window.scrollY - box.height - 8;
	control.bubble.style.position = "absolute";
	control.bubble.style.left = left + "px";
	control.bubble.style.top = (above > 0 ? above : rect.bottom + window.scrollY + 8) + "px";
	control.bubble.style.zIndex = "var(--pk-z-popover, 50)";
	updatePressed(control);
}

// bindSelectionChrome keeps the bubble answering the browser's own selection rather
// than waiting for the editor to notice one. The editor's selectionUpdate reports
// what the document decided and when the editor agreed with it; the keyboard's
// range is real — and reads as real to anything that asks the page — for a moment
// before that. A person who has selected words and is shown no toolbar has selected
// nothing as far as this field is concerned, which is this field's failure and not
// theirs. The listener is the page's, so it asks whether the range is inside this
// field's own surface before it moves anything: two fields on one page share the one
// document event, and a selection in the other one is not this one's business.
//
// What it does not settle is the editor's half. When the editor later re-asserts its
// own state over the browser's range, the bubble goes with it, and the paste that
// decides on the editor's state stands down — the selection-readiness race named in
// the task's follow-ups, which this listener narrows and does not close.
function bindSelectionChrome(control) {
	document.addEventListener("selectionchange", () => {
		const selection = window.getSelection();
		if (!selection || !selection.rangeCount || !control.surface.contains(selection.getRangeAt(0).commonAncestorContainer)) return;
		positionBubble(control);
	});
}

// bindSlash opens the command menu on a slash typed into an empty block, filters
// the drawn rows as the author keeps typing, and lets the keyboard choose one. The
// rows are the server's markup; only which of them are showing is computed here.
function bindSlash(control) {
	control.editor.on("update", () => {
		const empty = currentText(control);
		if (empty === "/") openMenu(control, "");
		else if (empty.startsWith("/")) filterMenu(control, empty.slice(1));
		else if (control.menu && !control.menu.hidden) closeMenu(control, true);
	});
}

function currentText(control) {
	const { $from } = control.editor.state.selection;
	if (!$from.parent.isBlock) return "";
	return $from.parent.textBetween(0, $from.parent.content.size, undefined, "\u{fffc}");
}

function openMenu(control, query) {
	if (!control.menu) return;
	control.menu.hidden = false;
	control.menu.style.position = "absolute";
	const caret = control.editor.view.coordsAtPos(control.editor.state.selection.from);
	control.menu.style.left = Math.max(8, caret.left + window.scrollX) + "px";
	control.menu.style.top = (caret.bottom + window.scrollY + 4) + "px";
	control.menuChoice = filterMenu(control, query);
}

function filterMenu(control, query) {
	const wanted = query.trim().toLowerCase();
	const rows = [...control.menu.querySelectorAll("[data-richtext-command]")];
	const shown = rows.filter((row) => wanted === "" || row.textContent.toLowerCase().includes(wanted));
	rows.forEach((row) => { row.hidden = !shown.includes(row); });
	if (shown.length === 0) { closeMenu(control); return []; }
	control.menuRows = shown;
	control.menuIndex = 0;
	markChoice(control);
	return shown;
}

function markChoice(control) {
	(control.menuRows || []).forEach((row, index) =>
		row.setAttribute("aria-selected", String(index === control.menuIndex)));
}

function menuKeys(control, event) {
	const rows = control.menuRows || [];
	if (event.key === "ArrowDown" || event.key === "ArrowUp") {
		const step = event.key === "ArrowDown" ? 1 : -1;
		control.menuIndex = (control.menuIndex + step + rows.length) % rows.length;
		markChoice(control);
		event.preventDefault();
		return true;
	}
	if (event.key === "Enter") {
		const chosen = rows[control.menuIndex];
		if (chosen) {
			// The slash the author typed to open the menu is part of the text, and it
			// is not content: it goes away with the menu that opened.
			deleteSlash(control);
			closeMenu(control);
			run(control, chosen.dataset.richtextCommand, true);
		}
		event.preventDefault();
		return true;
	}
	if (event.key === "Escape") {
		closeMenu(control, true);
		event.preventDefault();
		return true;
	}
	return false;
}

function deleteSlash(control) {
	const text = currentText(control);
	if (!text.startsWith("/")) return;
	const { from } = control.editor.state.selection;
	control.editor.chain().focus().deleteRange({ from: from - Math.max(1, text.length), to: from }).run();
}

function closeMenu(control, backspace) {
	if (!control.menu) return;
	control.menu.hidden = true;
	control.menuRows = [];
	if (backspace) deleteSlash(control);
}

function hideFloating(control) {
	closeMenu(control);
	hideLink(control);
	if (control.bubble) control.bubble.hidden = true;
}

// bindLink is the popover: one field, Enter accepts, and a URL pasted onto a
// selection is the same action with fewer keystrokes. The scheme list is §1's, and
// it is a hint given before the submit — the server refuses the same value again,
// and its refusal is the one that decides.
function bindLink(control) {
	if (!control.link || !control.linkInput) return;
	control.linkInput.addEventListener("keydown", (event) => {
		if (event.key === "Enter") { event.preventDefault(); saveLink(control); }
		if (event.key === "Escape") { event.preventDefault(); hideLink(control); control.editor.commands.focus(); }
	});
	bindPasteLink(control);
}

// bindPasteLink is the URL-over-a-selection spelling, and it is a capture-phase
// listener one rung above the element ProseMirror owns — the drawn surface —
// rather than the editor's `paste` event:
// ProseMirror parses a paste as soon as its handlers see it and replaces whatever
// was selected, so the selection the author means to link is gone by the time a
// listener downstream could answer. Answering before it is the only way to keep
// the words and add the address.
function bindPasteLink(control) {
	control.surface.addEventListener("paste", (event) => {
		const text = event.clipboardData ? event.clipboardData.getData("text/plain").trim() : "";
		if (!isLink(text) || control.editor.state.selection.empty) return;
		event.preventDefault();
		event.stopImmediatePropagation();
		linkSelection(control, text);
		commit(control);
	}, true);
}

function isLink(text) {
	if (!text) return false;
	const value = text.trim();
	if (value.startsWith("/") || value.startsWith("#")) return true;
	try { return LINK_SCHEMES.includes(new URL(value).protocol); } catch { return false; }
}

function openLink(control) {
	if (!control.link || !control.linkInput) return;
	const existing = control.editor.getAttributes("link");
	control.link.hidden = false;
	control.linkInput.value = existing.href || "";
	control.linkInput.focus();
	control.linkInput.select();
}

function saveLink(control) {
	const href = control.linkInput.value.trim();
	if (href === "") {
		removeLink(control);
	} else if (!isLink(href)) {
		mark(control, "error", { reason: "scheme" });
		control.status.textContent = word(control, "richtextBadLink");
		control.status.hidden = false;
		return;
	} else {
		linkSelection(control, href);
	}
	hideLink(control);
	commit(control);
}

// linkSelection and removeLink are the address applied to a selection. The bundled
// Link is a bare mark — it carries href and title and ships no setLink command —
// so the mark is set by name, which is also the only way a mark the editor does
// not know about could be applied at all.
function linkSelection(control, href) {
	const { state } = control.editor;
	const link = state.schema.marks.link;
	const { from, to } = state.selection;
	if (!link || from === to) return;
	control.editor.view.dispatch(state.tr.addMark(from, to, link.create({ href: href, title: null })).scrollIntoView());
}

function removeLink(control) {
	const { state } = control.editor;
	const link = state.schema.marks.link;
	const { from, to } = state.selection;
	if (!link || from === to) return;
	control.editor.view.dispatch(state.tr.removeMark(from, to, link).scrollIntoView());
}

function hideLink(control) {
	if (control.link) control.link.hidden = true;
}

// bindShortcuts implements the Markdown spellings as the author types them. Tiptap
// ships no input rules in the extensions this bundle carries, and the spellings are
// §1's contract with an author, so they live here: a few lines that turn the line
// the caret sits on into the block its prefix names.
// handleKey is the editor's whole keyboard surface: the open menu takes the keys
// first, Escape closes what is floating, and a space is where a block spelling
// ends. It is an editor prop rather than an event because this Tiptap emits no
// keydown event — a listener on one would never be called.
function handleKey(control, event) {
	if (control.menu && !control.menu.hidden) return menuKeys(control, event);
	if (event.key === "Escape") { hideFloating(control); return true; }
	return blockSpelling(control, event);
}

// blockSpelling turns the line the caret sits on into the block its prefix names,
// which is §1's contract with an author. The keystroke has not reached the document
// yet, so the line is read with the character being typed appended: the space that
// ends "##" is the last character of the spelling, not a character after it.
function blockSpelling(control, event) {
	if (event.key !== " " || control.editor.isActive("codeBlock")) return false;
	const line = currentText(control);
	const rules = [
		[/^## (.*)$/, () => run(control, "heading2")],
		[/^### (.*)$/, () => run(control, "heading3")],
		[/^- (.*)$/, () => run(control, "bulletList")],
		[/^1\. (.*)$/, () => run(control, "orderedList")],
		[/^- \[ \] (.*)$/, () => run(control, "taskList")],
		[/^> (.*)$/, () => run(control, "quote")],
		[/^```(.*)$/, () => run(control, "codeBlock")],
	];
	for (const [pattern, apply] of rules) {
		if (!pattern.test(line + " ")) continue;
		event.preventDefault();
		dropLine(control, line);
		apply();
		return true;
	}
	if (line === "---") {
		event.preventDefault();
		dropLine(control, line);
		control.editor.chain().focus().setHorizontalRule().run();
		return true;
	}
	return false;
}

// dropLine removes the spelling the author just typed, so the block that replaces
// it starts where the marker started.
function dropLine(control, text) {
	const { from } = control.editor.state.selection;
	control.editor.chain().focus().deleteRange({ from: from - text.length, to: from }).run();
}

// togglePreview is a read-only view of the same document. It is not the published
// page: only the server's renderer is that, and the page would have to name that
// route for this control to use it.
function togglePreview(control) {
	const previewing = control.editor.isEditable;
	control.editor.setEditable(!previewing, true);
	if (control.preview) {
		control.preview.hidden = !previewing;
		control.preview.textContent = word(control, "richtextPreviewNote");
	}
	updatePressed(control);
}

function pickFile(control) {
	if (control.file) control.file.click();
}

// bindUploads is the file path: the person's own bytes, the server's answer, and the
// one reference the document ever holds. A cancelled upload leaves the file to the
// file module's own sweep — deleting from a browser would be a retention decision
// made in the wrong place.
//
// Both the paste and the drop are answered on the drawn surface, in the capture
// phase, one rung above the element ProseMirror owns. ProseMirror parses a paste as
// soon as its own handlers see it and hands a listener what it built out of that
// parse, so a picture the browser was handed on the pasteboard is not in it; and a
// drop it reads as content. Answering first is what keeps the person's bytes theirs.
// bindPasteLink listens the same way for the address-over-a-selection spelling, and
// registers first, so a URL pasted across words is still a link rather than an
// upload.
function bindUploads(control) {
	const route = control.root.dataset.richtextUpload;
	if (!route) return;
	if (control.file) {
		control.file.addEventListener("change", () => {
			for (const file of [...control.file.files]) upload(control, file);
			control.file.value = "";
		});
	}
	const take = (event) => {
		const files = filesIn(event);
		if (files.length === 0) return false;
		event.preventDefault();
		event.stopImmediatePropagation();
		for (const file of files) upload(control, file);
		return true;
	};
	control.surface.addEventListener("paste", take, true);
	control.surface.addEventListener("dragover", (event) => { event.preventDefault(); });
	control.surface.addEventListener("drop", take);
}

// filesIn is the person's own files, from whichever half of the clipboard the
// browser filled: a paste carries them in clipboardData and a drag in dataTransfer.
// A reader of one of the two uploads on a drop and does nothing at all on a paste.
function filesIn(event) {
	const transfer = event.clipboardData || event.dataTransfer;
	if (!transfer) return [];
	if (transfer.items && transfer.items.length) {
		return [...transfer.items].filter((item) => item.kind === "file").map((item) => item.getAsFile()).filter(Boolean);
	}
	return transfer.files ? [...transfer.files] : [];
}

// upload posts one file as multipart with one filename-bearing part and all metadata
// in the query, which is the contract the file module answers. Progress comes from
// XMLHttpRequest because fetch has none, and the request is cancellable because the
// person may change their mind.
function upload(control, file) {
	const route = control.root.dataset.richtextUpload + "?image=true";
	const body = new FormData();
	body.append("file", file, file.name);
	const request = new XMLHttpRequest();
	const state = { file: file, request: request };
	request.open("POST", route, true);
	request.responseType = "text";
	request.upload.addEventListener("progress", (event) => {
		if (!event.lengthComputable) return;
		statusLine(control, word(control, "richtextUploading"),
			Math.round((event.loaded / event.total) * 100));
		mark(control, "uploading", { percent: Math.round((event.loaded / event.total) * 100) });
	});
	request.addEventListener("load", () => {
		if (request.status === 0) return refused(control, state, 0, "");
		if (request.status >= 200 && request.status < 300) return inserted(control, request, file);
		refused(control, state, request.status, problemDetail(request));
	});
	request.addEventListener("error", () => refused(control, state, 0, ""));
	request.addEventListener("abort", () => mark(control, "idle"));
	control.cancel = () => request.abort();
	request.send(body);
}

// The image reference and the address its pixels live at. A document holds the
// reference §1 stores and the server validates — ![alt](pk-file:<id>) — and a
// browser fetches bytes from no such address, so the page drew where they are: the
// file module's own content address with one hole in it for a file id. Which side
// of that pair a value sits on is decided once, in the image node's own src
// attribute, in both directions: the document, the Markdown and the posted value
// hold the reference, and only the drawn <img> holds the address. A reference no
// address was drawn for, and every other src, is drawn as it was written.
const fileReference = "pk-file:";
const fileSlot = "{file}";
const fileID = /^[0-9a-fA-F-]{36}$/;

// picture is the schema's image — block-level, no base64, the one §1's scheme
// carries — with that substitution and no other opinion of its own.
function picture(control) {
	return Image.extend({
		addAttributes() {
			return {
				...this.parent(),
				src: {
					default: null,
					renderHTML: (attributes) => ({ src: imageAddress(control, attributes.src) }),
					parseHTML: (node) => imageReference(control, node.getAttribute("src")),
				},
			};
		},
	});
}

// imageAddress is the reference read as an address for the browser to fetch from.
function imageAddress(control, src) {
	const id = referenceID(src);
	const address = control.root.dataset.richtextImageSrc || "";
	return address === "" || id === "" ? src : address.split(fileSlot).join(id);
}

// imageReference is an address read back as the reference, which is what keeps a
// picture copied out of the document and pasted into it again arriving as the line
// it left as rather than as an address the server would refuse.
function imageReference(control, src) {
	const halves = (control.root.dataset.richtextImageSrc || "").split(fileSlot);
	if (halves.length !== 2 || typeof src !== "string") return src;
	if (!src.startsWith(halves[0]) || !src.endsWith(halves[1])) return src;
	const id = src.slice(halves[0].length, src.length - halves[1].length);
	return referenceID(fileReference + id) === "" ? src : fileReference + id;
}

// referenceID is the file a reference names. Only an id of the shape the server
// accepts is named: nothing else reaches an address.
function referenceID(src) {
	if (typeof src !== "string" || !src.startsWith(fileReference)) return "";
	const id = src.slice(fileReference.length);
	return id.match(fileID) ? id : "";
}

// inserted is the only place a file reference enters the document, and it enters as
// the text §1 stores: ![alt](pk-file:<id>). The alt prompt is asked after the image
// is placed, because the person is more likely to answer it when the picture is
// where they can see it — and it is asked of the reference, which is how the answer
// finds this picture again after the author has written around it.
function inserted(control, request, file) {
	let id = "";
	try {
		const parsed = JSON.parse(request.responseText);
		id = parsed.id || (parsed.file && parsed.file.id) || "";
	} catch { id = ""; }
	if (id === "") return refused(control, { file: file }, 0, "");
	const src = fileReference + id;
	if (!placePicture(control, src)) {
		// The bytes are stored and the document would not take the picture. The page
		// drew no sentence for that, so the field says nothing it was not given and
		// leaves the state and the reference for the page to read.
		mark(control, "error", { reason: "picture not placed", src: src });
		return;
	}
	commit(control);
	askAlt(control, src);
}

// placePicture puts the picture an upload answered with into the document and says
// whether the document took it.
//
// A picture goes where the selection is, because that is where the author was
// writing. Two selections are not writing there, and to insert at a selection is to
// replace what it holds. An earlier picture of the same pick still waits for its
// description with the selection on it; and a picture the selection has simply come
// to rest on is held the same way, which is where the field leaves it when it places
// one and where a click or an arrow leaves any other. A second file chosen beside a
// first would then arrive by deleting the first, which is a picture nobody chose to
// lose — answered or not, whichever of the two has been described since. A picture
// the field is standing behind is therefore joined by the new one rather than
// replaced by it: the new picture goes directly after it.
function placePicture(control, src) {
	const behind = pictureToPlaceBehind(control);
	if (behind < 0) return control.editor.chain().focus().setImage({ src, alt: "", title: "" }).run()
		&& picturePosition(control.editor.state.doc, src) >= 0;
	const image = control.editor.schema.nodes.image;
	if (!image) return false;
	return control.editor.chain().command(({ tr }) => {
		const node = tr.doc.nodeAt(behind);
		if (!node || node.type.name !== "image") return false;
		tr.insert(behind + node.nodeSize, image.create({ src: src, alt: "", title: "" }));
		return picturePosition(tr.doc, src) >= 0;
	}).run();
}

function refused(control, state, status, detail) {
	const words = word(control, "richtextFailed");
	const retryable = status === 0 || status === 408 || status === 429 || status >= 500;
	statusLine(control, detail === "" ? words : words + " — " + detail, null);
	mark(control, "upload-failed", { status: status, detail: detail, retry: retryable });
	offer(control, {
		note: detail === "" ? words : words + " — " + detail,
		actions: retryable
			? [{ label: word(control, "richtextRetry"), act: () => upload(control, state.file) }]
			: [{ label: word(control, "richtextChoose"), act: () => pickFile(control) }],
	});
}

function problemDetail(request) {
	try {
		const problem = JSON.parse(request.responseText);
		return problem.detail || problem.title || "";
	} catch { return ""; }
}

// askAlt is the prompt on insert: alt text is required, or the picture is
// decorative, which is the same answer with a different reason.
//
// Three things make it a question rather than a pair of boxes. Each control is
// named by a label joined to it by identity, because the prompt is read by people
// who hear the page. Which of the two answers is being given is decided here and
// not by the input's own `required`, which constrains a form's submit and not a
// button of type=button: an empty answer is a refusal until the author has said the
// picture is decorative, and is the whole answer once they have. And Escape is not
// an answer: it withdraws the picture the prompt was asked for, because the document
// that carries an undescribed photograph is the thing the requirement exists to
// prevent.
//
// The prompt is not modal: the author types, deletes and undoes while it waits. It
// therefore carries the file reference its picture arrived with rather than the
// position it arrived at — a reference names one picture in this document and
// survives every edit between the upload's answer and that answer's being given,
// and a position survives none of them.
function askAlt(control, src) {
	if (word(control, "richtextAltTitle") === "" || word(control, "richtextSave") === "") return;
	awaitDescription(control, src);
	control.altAsked = (control.altAsked || 0) + 1;
	const descriptionID = control.field + "-richtext-alt-" + control.altAsked;
	const decorativeID = descriptionID + "-decorative";
	const input = element("input", { type: "text", id: descriptionID, required: "" });
	const label = element("label", { for: descriptionID }, word(control, "richtextAltTitle"));
	const decorative = element("input", { type: "checkbox", id: decorativeID });
	const decorativeLabel = element("label", { for: decorativeID }, [decorative, word(control, "richtextDecorative")]);
	const save = element("button", { type: "button" }, word(control, "richtextSave"));
	const prompt = element("div", { role: "group", "aria-label": word(control, "richtextAltTitle") },
		[element("div", {}, [label, input, decorativeLabel, save])]);
	const open = (control.openPrompts || []).filter((waiting) => waiting.isConnected);
	// Two pictures asked for at once are answered in the order they were picked, so
	// a prompt goes behind the last one still open rather than in front of it.
	control.surface.parentElement.insertBefore(prompt, (open.length ? open[open.length - 1] : control.surface).nextSibling);
	control.openPrompts = open.concat([prompt]);
	input.focus();
	const close = () => {
		prompt.remove();
		control.openPrompts = (control.openPrompts || []).filter((waiting) => waiting !== prompt);
	};
	const finish = () => {
		const decorativeAnswer = decorative.checked;
		if (!decorativeAnswer && input.value.trim() === "") {
			mark(control, "error", { reason: "alt" });
			statusLine(control, word(control, "richtextAltRequired"), null);
			input.focus();
			return;
		}
		// A decorative picture's answer is the empty description — the alternative the
		// question offers is not an unanswered one, and refusing it would make the
		// accessibility choice the field offers impossible to complete.
		const answered = describePicture(control, src, { alt: decorativeAnswer ? "" : input.value.trim() });
		forgetUndescribed(control, src);
		close();
		if (answered) return commit(control);
		// The picture this question was asked about left the document while the answer
		// was being written — deleted, or undone back over the insert. The prompt says
		// so rather than closing as though it had described something: an author who
		// watched the picture go needs the field to agree with what they saw, not to
		// answer a question that no longer has a subject.
		statusLine(control, word(control, "richtextAltGone"), null);
		mark(control, "error", { reason: "picture gone", src: src });
	};
	save.addEventListener("click", finish);
	input.addEventListener("keydown", (event) => { if (event.key === "Enter") { event.preventDefault(); finish(); } });
	decorative.addEventListener("change", () => { input.required = !decorative.checked; });
	prompt.addEventListener("keydown", (event) => {
		if (event.key !== "Escape") return;
		event.preventDefault();
		close();
		forgetUndescribed(control, src);
		withdrawPicture(control, src);
		commit(control);
	});
}

// pictureToPlaceBehind is the picture a new one must not land on top of: the newest
// one still waiting for its description, or, once every question has been answered,
// the picture the selection itself is holding. -1 when the document holds neither,
// which is an author writing words, and the ordinary case.
function pictureToPlaceBehind(control) {
	const waiting = lastUndescribedPosition(control);
	if (waiting >= 0) return waiting;
	return selectedPicturePosition(control);
}

// selectedPicturePosition is where the selection covers one whole picture, or -1
// when it covers words. The picture has to be all the selection spans, at its own
// address and for all of its own length: a range that reaches past a picture is the
// author having selected around it, and the picture they asked for belongs where
// they pointed rather than behind what they highlighted.
function selectedPicturePosition(control) {
	const { from, to } = control.editor.state.selection;
	if (from === to) return -1;
	let at = -1;
	control.editor.state.doc.nodesBetween(from, to, (node, position) => {
		if (at >= 0) return false;
		if (node.type.name === "image" && position === from && position + node.nodeSize === to) at = position;
		return false;
	});
	return at;
}

// awaitDescription and forgetUndescribed keep the pictures this field placed and
// has not yet described, in the order they arrived. One thing reads the list: the
// next upload, which has to know it follows one of them rather than replacing one.
// Answering a prompt never consults it — the prompt carries the reference its own
// picture matched on, which is a name and not a seat.
function awaitDescription(control, src) {
	control.undescribed = (control.undescribed || []).concat([src]);
}

function forgetUndescribed(control, src) {
	control.undescribed = (control.undescribed || []).filter((waiting) => waiting !== src);
}

// lastUndescribedPosition is where the newest picture still waiting sits, or -1
// when this document holds none of them. The position is looked up from the
// reference each time rather than remembered, which is the whole point of the
// reference: the author's keystrokes move a picture and never rename one.
function lastUndescribedPosition(control) {
	for (const src of (control.undescribed || []).slice().reverse()) {
		const at = picturePosition(control.editor.state.doc, src);
		if (at >= 0) return at;
	}
	return -1;
}

// picturePosition is where one document holds the picture a file reference names,
// or -1 when it holds none. The reference is the file's own id, so in one document
// it names one picture and no other.
function picturePosition(doc, src) {
	let at = -1;
	doc.descendants((node, position) => {
		if (at >= 0) return false;
		if (node.type.name !== "image" || node.attrs.src !== src) return;
		at = position;
		return false;
	});
	return at;
}

// describePicture answers the prompt for the one picture it was asked about, and
// says whether that picture is still in the document to be answered for. Only the
// description is written: the picture's own title stays as the document holds it,
// which is never the title of whichever picture the caret happens to reach now.
function describePicture(control, src, attributes) {
	return control.editor.chain().command(({ tr }) => {
		const at = picturePosition(tr.doc, src);
		const node = at < 0 ? null : tr.doc.nodeAt(at);
		if (!node || node.type.name !== "image") return false;
		tr.setNodeMarkup(at, undefined, Object.assign({}, node.attrs, attributes));
		return true;
	}).run();
}

// withdrawPicture is the other half of the prompt: the picture goes back the way it
// came when the question that came with it is withdrawn rather than answered.
function withdrawPicture(control, src) {
	control.editor.chain().command(({ tr }) => {
		const at = picturePosition(tr.doc, src);
		const node = at < 0 ? null : tr.doc.nodeAt(at);
		if (!node || node.type.name !== "image") return false;
		tr.delete(at, at + node.nodeSize);
		return true;
	}).run();
}

// offer is the one line that can carry a button: a failed upload, a conflict, a
// draft that came back from another tab. It replaces the last one rather than
// stacking, because a form with three error widgets is a form nobody reads.
function offer(control, spec) {
	const slot = control.offer;
	if (!slot) return;
	const actions = element("div", { role: "group", "aria-label": word(control, "richtextChoices") });
	for (const action of spec.actions) {
		if (!action.label) continue;
		const button = element("button", { type: "button" }, action.label);
		button.addEventListener("click", action.act);
		actions.append(button);
	}
	slot.replaceChildren(element("p", {}, spec.note), actions);
	slot.hidden = false;
	slot.querySelector("button")?.focus();
}

function statusLine(control, text, percent) {
	if (!control.status || text === "") return;
	control.status.hidden = false;
	control.status.textContent = percent === null ? text : text + " " + percent + "%";
}

function element(tag, attributes, content) {
	const node = document.createElement(tag);
	for (const name in attributes) node.setAttribute(name, attributes[name]);
	if (typeof content === "string") node.textContent = content;
	if (Array.isArray(content)) node.append(...content);
	return node;
}

// bindPersistence is the save half. The page decided which mode this field is in:
// an endpoint and a tag mean the record takes an expected version and the editor
// saves as the author writes; no endpoint means nothing is sent and the draft is
// kept on this device instead. The two are never tried at once.
function bindPersistence(control) {
	control.endpoint = control.root.dataset.richtextEndpoint || "";
	control.etag = control.root.dataset.richtextEtag || "";
	control.key = control.root.dataset.richtextDraft || "";
	// A form redrawn after a write the server refused carries the person's own
	// submission in its controls. That text says nothing about the record — the page
	// says which of the two it is showing, and nothing compares a device's copy
	// against an echo.
	control.echoed = control.root.dataset.richtextSubmitted !== undefined;
	if (control.endpoint === "" && control.key !== "" && !control.echoed) restoreOffered(control);
	window.addEventListener("storage", (event) => {
		if (event.key === control.key && event.newValue) restoreOffered(control);
	});
	for (const button of control.root.querySelectorAll("[data-richtext-draft-restore]")) {
		button.addEventListener("click", () => {
			const stored = readDraft(control.key);
			if (!stored) return;
			load(control, stored.markdown);
			control.draftBar.hidden = true;
			mark(control, "dirty");
		});
	}
	for (const button of control.root.querySelectorAll("[data-richtext-draft-discard]")) {
		button.addEventListener("click", () => {
			localStorage.removeItem(control.key);
			control.draftBar.hidden = true;
			mark(control, "idle");
		});
	}
	const form = control.textarea.form;
	// A submit commits the value and touches no draft. A form posts and navigates, so
	// at the moment of the submit nothing has been answered — and the write that came
	// back refused leaves the words unsaved with this copy as the only place they
	// live. Clearing a draft is the record's doing: see restoreOffered.
	if (form) form.addEventListener("submit", () => commit(control));
}

function save(control) {
	if (control.dirty === undefined) control.dirty = true;
	if (control.endpoint !== "") return autosave(control);
	keepDraft(control, false);
}

// keepDraft writes the browser's own copy. A browser that will not hold it is said
// to out loud: "Saved on this device" is a promise this one did not keep.
function keepDraft(control, cleared) {
	if (control.key === "") return;
	if (cleared) { localStorage.removeItem(control.key); mark(control, "saved"); return; }
	try {
		localStorage.setItem(control.key, JSON.stringify({ markdown: control.textarea.value, savedAt: Date.now(), doc: DOCUMENT_SHAPE }));
		mark(control, "drafted");
	} catch {
		mark(control, "offline");
		statusLine(control, word(control, "richtextNoDraft"), null);
	}
}

function readDraft(key) {
	try {
		const stored = JSON.parse(localStorage.getItem(key) || "null");
		return stored && stored.doc === DOCUMENT_SHAPE ? stored : null;
	} catch { return null; }
}

function restoreOffered(control) {
	const stored = readDraft(control.key);
	if (!stored) return;
	// The record was drawn carrying the words this device kept, so the write that
	// carried them was answered and nothing is left unsaved: the copy goes, and the
	// bar has nothing to offer. This is the only thing that clears a draft besides the
	// person's own Discard button, and it is evidence rather than optimism — the
	// moment a form is posted says nothing about whether the write it asked for
	// happened, and a refused one leaves this copy as the only place the words are.
	if (savedAlready(stored.markdown, control.textarea.value)) {
		if (!control.echoed) keepDraft(control, true);
		return;
	}
	if (!control.draftBar || control.echoed) return;
	control.draftBar.hidden = false;
	const when = new Date(stored.savedAt);
	const label = Number.isNaN(when.getTime()) ? "" : " " + when.toLocaleTimeString(document.documentElement.lang || undefined, { hour: "2-digit", minute: "2-digit" });
	control.draftBar.firstElementChild.textContent = (word(control, "richtextDraftFound")) + label;
}

// savedAlready asks whether the record says what the device kept, reading the words
// rather than the bytes: line endings differ by browser, and the server's own
// normalisation ends a body with a newline. It is asked only of a value that came
// out of a record, never of an echo of a refused write.
function savedAlready(stored, drawn) {
	const said = (text) => String(text).replace(/\r\n?/g, "\n").trim();
	return said(stored) === said(drawn);
}

// autosave writes one field and quotes the tag this form was drawn from. A body
// carrying a second field would take somebody else's edit down with it, and a tag
// invented here would be a conflict the client decided for the server.
async function autosave(control) {
	if (control.saving) { control.queued = true; return; }
	control.saving = true;
	mark(control, "saving");
	let response;
	try {
		response = await fetch(control.endpoint, {
			method: "PATCH",
			headers: { "Content-Type": "application/json", "If-Match": control.etag },
			body: JSON.stringify({ [control.field]: control.textarea.value }),
			credentials: "same-origin",
		});
	} catch {
		control.saving = false;
		mark(control, "offline");
		return;
	}
	const tag = response.headers.get("ETag");
	if (tag) control.etag = tag.replace(/^W\//, "");
	control.saving = false;
	if (response.status === 409 || response.status === 412) {
		mark(control, "conflict");
		compare(control);
	} else if (!response.ok) {
		mark(control, "error", { status: response.status });
		statusLine(control, problemDetail(response) || word(control, "richtextRefused"), null);
	} else {
		mark(control, "saved");
	}
	if (control.queued) { control.queued = false; save(control); }
}

// compare is the conflict panel. Both texts are in the page, neither is discarded,
// and the choice is the author's: keep theirs, take what arrived, or close the
// panel and merge by hand against the tag that just arrived.
async function compare(control) {
	let theirs = "";
	let tag = control.etag;
	try {
		const read = await fetch(control.endpoint, { credentials: "same-origin" });
		theirs = JSON.stringify(await read.json(), null, 0);
		tag = read.headers.get("ETag") || tag;
	} catch { theirs = ""; }
	const theirsText = JSON.parse(theirs || "null");
	offer(control, {
		note: word(control, "richtextConflict"),
		actions: [
			{ label: word(control, "richtextKeep"), act: () => { control.etag = tag; save(control); } },
			{ label: word(control, "richtextTheirs"), act: () => {
				if (theirsText && typeof theirsText[control.field] === "string") load(control, theirsText[control.field]);
				control.etag = tag;
			} },
			{ label: word(control, "richtextMerge"), act: () => { control.etag = tag; mark(control, "dirty"); } },
		],
	});
	control.conflicted = true;
}

// load puts a text into both halves of the field: the editor's document and the
// value the form would post if the browser had no editor at all.
function load(control, markdown) {
	control.editor.commands.setContent(markdown, { contentType: "markdown" });
	control.textarea.value = markdown;
	control.textarea.dispatchEvent(new Event("change", { bubbles: true }));
	count(control);
}

// The page's fields are mounted here, at the foot of the module, and not at its
// head: building an editor for a field whose document already holds a picture reads
// the file-reference constants further up this file while it parses that picture,
// and a const the module has not evaluated yet throws instead of mounting. Functions
// are hoisted and constants are not, so a mount at the head works on an empty field
// and fails on one that already holds a picture.
for (const root of document.querySelectorAll("[data-richtext]")) {
	mount(root);
}
