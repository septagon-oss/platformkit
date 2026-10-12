// The one mark decision 0069 §1 rules that no published Tiptap extension
// carries in the shape the kernel stores: a link.
//
// @tiptap/extension-link is not a dependency of this bundle. It costs 13 KB of
// the minified output on its own because it drags linkifyjs along to autolink
// bare text — a rewrite of what the author typed, which the stored format does
// not want — and it validates schemes with a wider list than §1 allows, while
// kit/richtext already refuses an off-list scheme at the write. What is left is
// a mark over href and title that prints `[text](href "title")`, which is what
// this file is: about forty lines, and the only first-party code in the bundle.
import { Mark, mergeAttributes } from "@tiptap/core";

export const Link = Mark.create({
  name: "link",

  // A link does not extend to text typed at either end of it.
  inclusive: false,
  keepOnSplit: false,

  addAttributes() {
    return {
      href: { default: null, parseHTML: (element) => element.getAttribute("href") },
      title: { default: null, parseHTML: (element) => element.getAttribute("title") },
    };
  },

  parseHTML() {
    return [{ tag: "a[href]" }];
  },

  renderHTML({ HTMLAttributes }) {
    const { href, title } = HTMLAttributes;
    const attrs = { href };
    if (title) attrs.title = title;
    return ["a", mergeAttributes(attrs), 0];
  },

  // The Markdown extension renders a mark as the text around a placeholder, so
  // this returns the whole form and the manager splits it at the content.
  renderMarkdown(node, helpers) {
    const href = (node.attrs && node.attrs.href) || "";
    const title = node.attrs && node.attrs.title;
    const label = href === "" ? helpers.renderChildren(node) : `[${helpers.renderChildren(node)}](${href}${title ? ` "${quotedTitle(title)}"` : ""})`;
    return label;
  },

  parseMarkdown(token, helpers) {
    return helpers.applyMark(
      "link",
      helpers.parseInline(token.tokens || []),
      { href: token.href, title: token.title || null },
    );
  },
});

// quotedTitle writes a title back the way it was read. A title arrives with its
// quotes already taken off — `A "quoted" title` — and putting those bytes back
// between two quote characters ends the title at the author's first quote: what
// is left is an address with an unterminated title after it, which no reader
// parses as a link. The author's words then survive nowhere — the page shows an
// address where a word was written — so the two characters that carry meaning at
// this position are written as themselves. Backslash first, so the escape it
// earns is not escaped by the pass that follows it.
function quotedTitle(title) {
  return String(title).replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}
