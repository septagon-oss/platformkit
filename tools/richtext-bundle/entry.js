// The richtext bundle's single entry. Everything esbuild pulls into
// ui/assets/richtext/editor.js arrives through this file, so the set of
// third-party code a page loads is one readable list rather than whatever the
// glue happened to import.
//
// The list is decision 0069 §1's scheme as Tiptap extensions: the block and
// mark set, plus history, the placeholder and the character count the field's
// chrome needs. Tiptap ships no CSS, so nothing here draws a pixel: every
// control the author sees is drawn by ui/forms and styled by the sheet.
//
// Two named absences. StarterKit is not used because it bundles the extensions
// §1 excludes (underline, subscript, superscript, paragraph merge on paste,
// trailing whitespace) and a page should not carry what it must refuse.
// @tiptap/extension-link is not used, for the reasons in schema.js.
//
// "Insert from the library" (T-0316) will add one more entry point to the same
// command vocabulary; it changes nothing here.
export { Editor, Extension, Node, Mark, mergeAttributes } from "@tiptap/core";
export { Markdown } from "@tiptap/markdown";
export { default as Blockquote } from "@tiptap/extension-blockquote";
export { default as Bold } from "@tiptap/extension-bold";
export { default as BulletList } from "@tiptap/extension-bullet-list";
export { default as CharacterCount } from "@tiptap/extension-character-count";
export { default as Code } from "@tiptap/extension-code";
export { default as CodeBlock } from "@tiptap/extension-code-block";
export { default as Document } from "@tiptap/extension-document";
export { default as HardBreak } from "@tiptap/extension-hard-break";
export { default as Heading } from "@tiptap/extension-heading";
export { default as History } from "@tiptap/extension-history";
export { default as HorizontalRule } from "@tiptap/extension-horizontal-rule";
export { default as Image } from "@tiptap/extension-image";
export { default as Italic } from "@tiptap/extension-italic";
export { default as ListItem } from "@tiptap/extension-list-item";
export { default as OrderedList } from "@tiptap/extension-ordered-list";
export { default as Paragraph } from "@tiptap/extension-paragraph";
export { default as Placeholder } from "@tiptap/extension-placeholder";
export { default as Strike } from "@tiptap/extension-strike";
export { default as TaskItem } from "@tiptap/extension-task-item";
export { default as TaskList } from "@tiptap/extension-task-list";
export { default as Text } from "@tiptap/extension-text";
export { TableKit } from "@tiptap/extension-table/kit";
export { Link } from "./schema.js";
