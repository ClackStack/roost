---
name: Roost
description: A hand-drawn zine explainer in photocopied paper, black marker and flat highlighter.
colors:
  yellow: "#ffe84d"
  pink: "#ff6fae"
  teal: "#2dd4bf"
  paper: "#fffdf4"
  white: "#ffffff"
  ink: "#111111"
  ink-soft: "#3b3a36"
typography:
  display:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "clamp(2.5rem, min(5.9vw, 11.5vh), 5.6rem)"
    fontWeight: 400
    lineHeight: 1.04
  headline:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "clamp(2.25rem, 1.5rem + 3.2vw, 4.25rem)"
    fontWeight: 400
    lineHeight: 1.08
  title:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "clamp(1.5rem, 1.25rem + 0.9vw, 2rem)"
    fontWeight: 400
    lineHeight: 1.08
  caption:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "clamp(1.3rem, 1rem + 0.8vw, 1.7rem)"
    fontWeight: 400
    lineHeight: 1.15
  scrawl:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "1.35rem"
    fontWeight: 400
    lineHeight: 1.15
  button:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "1.3rem"
    fontWeight: 400
    lineHeight: 1.1
  nav:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "1.3rem"
    fontWeight: 400
    lineHeight: 1
  tab:
    fontFamily: "Permanent Marker, Karla, sans-serif"
    fontSize: "1.15rem"
    fontWeight: 400
    lineHeight: 1
  body:
    fontFamily: "Karla, sans-serif"
    fontSize: "clamp(1.0625rem, 0.98rem + 0.3vw, 1.25rem)"
    fontWeight: 500
    lineHeight: 1.5
  body-strong:
    fontFamily: "Karla, sans-serif"
    fontSize: "clamp(1.0625rem, 0.98rem + 0.3vw, 1.25rem)"
    fontWeight: 700
    lineHeight: 1.5
  code-block:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "clamp(0.82rem, 0.78rem + 0.2vw, 0.95rem)"
    fontWeight: 400
    lineHeight: 1.6
  chip:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "0.9rem"
    fontWeight: 400
rounded:
  code: "4px"
  chip: "6px"
  frame: "10px"
  drawn: "10px 7px 12px 6px"
  drawn-button: "11px 7px 13px 6px"
  drawn-tab: "12px 8px 14px 7px"
spacing:
  xs: "8px"
  sm: "12px"
  md: "14px"
  lg: "16px"
  xl: "28px"
  gutter: "clamp(16px, 4vw, 56px)"
  column-gap: "clamp(28px, 5vw, 72px)"
  section: "clamp(64px, 9vw, 128px)"
  insert-section: "clamp(40px, 6vw, 88px)"
components:
  button-primary:
    backgroundColor: "{colors.yellow}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.frame}"
    padding: "0.5em 1.1em"
    height: "52px"
  tab:
    backgroundColor: "{colors.white}"
    textColor: "{colors.ink}"
    typography: "{typography.tab}"
    rounded: "{rounded.drawn-tab}"
    padding: "0 14px"
    height: "48px"
  tab-hover:
    backgroundColor: "{colors.paper}"
  tab-selected:
    backgroundColor: "{colors.teal}"
  chip-example:
    textColor: "{colors.ink}"
    typography: "{typography.chip}"
    rounded: "{rounded.drawn}"
    padding: "0 12px"
    height: "44px"
  chip-example-selected:
    backgroundColor: "{colors.yellow}"
  chip-locator:
    backgroundColor: "{colors.teal}"
    textColor: "{colors.ink}"
    rounded: "{rounded.chip}"
    padding: "0 8px"
  frame-card:
    backgroundColor: "{colors.white}"
    textColor: "{colors.ink}"
    rounded: "{rounded.frame}"
    padding: "clamp(16px, 2.4vw, 28px)"
  field-search:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.frame}"
    padding: "0 16px"
    height: "52px"
  select-platform:
    backgroundColor: "{colors.white}"
    textColor: "{colors.ink}"
    rounded: "{rounded.frame}"
    padding: "0 42px 0 16px"
    height: "52px"
  code-block:
    backgroundColor: "{colors.white}"
    textColor: "{colors.ink}"
    typography: "{typography.code-block}"
    rounded: "{rounded.frame}"
    padding: "1rem"
  nav-link:
    textColor: "{colors.ink}"
    typography: "{typography.nav}"
    height: "44px"
  insert-yellow:
    backgroundColor: "{colors.yellow}"
    textColor: "{colors.ink}"
  insert-teal:
    backgroundColor: "{colors.teal}"
    textColor: "{colors.ink}"
  insert-pink:
    backgroundColor: "{colors.pink}"
    textColor: "{colors.ink}"
---

# Design System: Roost

## Overview

**Creative North Star: "The Sketch for a Friend"**

Roost looks the way a good engineer explains something on paper to someone they like: quickly, in black marker, with a highlighter out for the line that matters. Everything sits on photocopied paper with visible toner grain. Full-bleed sheets of highlighter colour are stapled in between the paper pages, and a small pink bird reacts to what the page is saying.

The drawing is loose but the reading is not. Display lettering is loud and hand-made; anything that has to be read properly drops into a clean, generously sized sans. Frames wobble, but the text inside them stays perfectly crisp. Density is comfortable, with large body type, wide section spacing and plain ink on every surface.

The system refuses the dark terminal hero and the row of icon cards. Code sits on white like everything else, and icons are doodles beside text, not tiles.

**Key Characteristics:**
- Warm paper with toner grain, never a dark theme.
- Three flat highlighters and one black marker. No fourth hue.
- Marker lettering for voice, Karla for reading, JetBrains Mono for commands and config.
- Wobbly drawn lines on their own layer, so the text stays sharp.
- A bird mascot whose expression carries state.
- Depth comes from stacked sheets of paper, not from floating UI.

## Colors

A photocopier palette: near-white paper and near-black ink, with three saturated highlighters doing all the colour work.

### Primary
- **Highlighter Yellow** (#ffe84d): The action and emphasis colour. Fills the primary button, the default swipe, the marked line in code, search hits, the selected example chip, the Roost column in comparisons, and one full-bleed sheet.

### Secondary
- **Mascot Pink** (#ff6fae): The bird's body, the swipe on the most important or most painful line, data bars, the brick wall, the crossed-out X in illustrations, the hover colour for link underlines, the text caret, and one full-bleed sheet.

### Tertiary
- **Marker Teal** (#2dd4bf): The quiet third pen. Fills the selected tab and locator chips, the hand-drawn heading underline, illustration fills, the bird's teardrops, and one full-bleed sheet. At 1.83:1 against paper it is a fill, never a text colour.

### Neutral
- **Photocopy Paper** (#fffdf4): The page, always under the toner grain tile. Also the search field inside a white card and the hover state of a tab.
- **Clean Sheet White** (#ffffff): Cards, code blocks, tables and anything that needs to sit forward of the paper or a coloured sheet.
- **Marker Ink** (#111111): All text, all lines, all icons. Holds at least 7.32:1 on every surface in the system. An 8% wash of it backs inline code, and a 13% version prints the halftone dots on coloured sheets.
- **Faded Ink** (#3b3a36): Fine print and notes on paper or white only. It falls to 4.41:1 on pink.

### Named Rules

**The Three Highlighters Rule.** Yellow, pink and teal are the only hues. A fourth colour means a pen the world doesn't own.

**The Black Marker Rule.** Text is ink on every surface, coloured sheets included. White text on a highlighter does not exist here.

## Typography

**Display Font:** Permanent Marker (with Karla, sans-serif)
**Body Font:** Karla (with sans-serif)
**Label/Mono Font:** JetBrains Mono (with ui-monospace, monospace)

**Character:** A fat, slanted felt-tip that always sounds like a person, set against a friendly grotesque with enough weight to survive a toner-speckled page. All three families are self-hosted, latin subset only: Permanent Marker 400, Karla 500 and 700, JetBrains Mono 400.

### Hierarchy
- **Display** (400, up to 5.6rem, 1.04): The page headline only. The one role set in uppercase.
- **Headline** (400, up to 4.25rem, 1.08): Section headings, balanced across lines.
- **Title** (400, up to 2rem, 1.08): Sub-headings inside a section.
- **Caption** (400, up to 1.7rem, 1.15): Numbered comic panel captions, kept to about two lines.
- **Scrawl** (400, 1.35rem, 1.15): Margin notes beside a doodle arrow and the bird's replies.
- **Button, Nav and Tab** (400, 1.15 to 1.3rem, 1 to 1.1): Marker lettering on controls.
- **Body** (500, up to 1.25rem, 1.5): All reading text, capped at 62ch, or 56ch at 1.15em for section ledes. Numerals are tabular.
- **Body Strong** (700): Row labels, list lead-ins, form values, code captions.
- **Code** (400, 0.88em inline, up to 0.95rem in blocks at 1.6): Commands, config, tool names and example queries.

Big proof numbers use the marker at 1.6rem to 3rem with a line height of 1, so a figure reads as if written by hand.

### Named Rules

**The Marker Speaks, Karla Explains Rule.** Marker carries headings, labels, numbers and short remarks. Any sentence a reader has to study is Karla, including every claim of fact longer than a caption.

**The Run-It-Yourself Rule.** Commands and config a reader would run or paste are JetBrains Mono, so anything checkable looks checkable.

## Layout

Content sits in a centred column up to 1440px wide, inside a fluid gutter (16px to 56px). Paper sections breathe at 64px to 128px of block padding, and sheet content tightens to 40px to 88px because the sheet edge already marks the break. Two-column grids split evenly, or 5:6 when a toy sits beside its proof, with a fluid 28px to 72px gap. Small gaps are set by eye from 8, 12, 14, 16 and 28px rather than a strict scale.

The page alternates paper and sheets, and sheets always run the full width of the viewport. Each sheet's background extends 3% past both edges so its tilt never exposes a corner.

The masthead sticks to the top on wide screens, with the same toner paper as the page, and anchors land 88px below it. At 960px and under, two-column sections stack. At 860px the masthead stops sticking and multi-panel rows stack. At 720px comparison tables turn into one labelled card per row, keeping the yellow Roost line. At 520px paired controls wrap to full width.

Every control is at least 44px tall; inline text links are the exception. Long code scrolls inside its own block and never widens the page.

## Elevation & Depth

Flat drawn lines by default. Cards, buttons and fields have no shadow, and they separate from the page through a marker outline and a white fill. The only real depth in the system is physical: a coloured sheet resting on the paper, tilted a fraction of a degree, held by two drawn staples. The soft shadow under the sheet goes beyond the flat photocopy the world started from, but it is what makes the sheet read as an insert, so it stays, for that job alone.

Surface texture does the rest. Paper carries a tiled toner grain (a procedurally generated 320px PNG), and sheets layer that grain over a 15px halftone dot grid.

### Shadow Vocabulary
- **Sheet rest** (`box-shadow: 0 3px 14px rgb(17 17 17 / 28%)`): Under full-bleed coloured sheets only.
- **Focus halo** (`box-shadow: 0 0 0 3px var(--white)`): A white ring that separates the focus outline from coloured surfaces. Functional, not depth.

### Named Rules

**The Stapled Sheet Rule.** Only a stapled sheet gets a shadow. If something else needs to stand out, give it a marker frame and a white fill.

## Shapes

Every drawn form is a marker line, so nothing is geometrically perfect. Corners are gently rounded and deliberately uneven (6px to 14px, four different values per box). Frames use a 3.5px ink line roughened by one shared SVG displacement filter (`#wob`: turbulence 0.035, 2 octaves, seed 4, scale 4). The same filter runs over illustrations, doodle arrows and icons.

Tabs and chips skip the filter and rely on uneven corners alone, at 2.5px to 3px. The bird, staples, chevrons and list markers are also unfiltered.

Rows inside a frame or list are divided by 2px dashed ink lines, with a 3px solid line under headers and captions. Highlighter shapes come from a single torn-edge swipe, used as a mask so its colour stays a token.

### Named Rules

**The Wobble Layer Rule.** The wobble never touches text. The line lives on its own layer (a pseudo-element or an SVG group), and labels sit outside it.

**The Second Pass Rule.** A button is outlined twice, the way you go over a box twice with a marker: a 3.5px line, then a thinner 2px line offset about 3px down and right. The second pass is an unfilled, wobbled line. It belongs to buttons, and it never fills in to become a solid offset block.

## Components

### Buttons
The one loud control: a yellow box drawn twice.
- **Shape:** Slightly uneven corners, drawn with the Second Pass (see Shapes), at least 52px tall.
- **Primary:** Yellow fill, ink marker label. There is no secondary button; lesser actions are text links or tabs.
- **Hover / Focus:** Tilts -1.2deg and grows 3% over 160ms on a fast ease-out. Presses to 98%. Focus uses the global ring.
- **Disabled:** 60% opacity with a not-allowed cursor.

### Tabs
- **Style:** White with a 3px ink line and uneven corners, marker label, 48px tall.
- **State:** The selected tab fills teal and exposes `aria-pressed`. Hover turns the tab paper and tilts it -1deg.

### Chips
- **Example chip:** Unfilled, 2.5px ink line, mono label, 44px tall. Hover and selected both fill yellow.
- **Locator chip:** A small teal tag in bold Karla that names a page, slide, sheet or heading. Not interactive.

### Cards / Containers
- **Corner Style:** Softly rounded 10px box with the uneven drawn line on top.
- **Background:** White, on paper or on a coloured sheet.
- **Shadow Strategy:** None. See Elevation & Depth.
- **Border:** The wobbled 3.5px marker frame.
- **Internal Padding:** 14px to 18px for notes and panels, fluid 16px to 28px for toys.

### Inputs / Fields
- **Style:** A marker frame around a borderless 52px control with bold Karla text. Search fields fill paper to sit back inside a white card, and selects fill white with a drawn chevron.
- **Focus:** The global ring (4px ink outline, 3px offset, white halo). The search caret is pink.

### Navigation
Lowercase source text in marker, spaced 26px apart, 44px tall targets. Hover wipes a yellow highlighter band in behind the word over 220ms, and the download link keeps its band on permanently. The logo pairs the bird with the name at 2rem. The row wraps and shrinks to 1rem with 10px gaps on the narrowest screens, and it never collapses into a menu.

### Links
Inherit ink, with a 2px underline offset by 4px. Hover turns the underline pink and thickens it to 4px.

### Code Blocks
A framed white figure with a bold Karla caption and a Copy text button, split from the code by a 3px ink line. The line that matters takes a full-width yellow bar. Copy reports its result in the button label for 2 seconds. Blocks are always light.

### Highlighter Swipe
The world's emphasis device. A torn-edge highlight behind a phrase, yellow by default and pink when the line is the most important or the most painful on the page. Use one per heading at most. The finished swipe is the default state: with scroll-driven animation it wipes in from the left, and the headline swipe plays once on load over 700ms. A hand-drawn teal underline is the quieter alternative.

### Doodle Arrow and Scrawl
A wobbled 3.5px to 4px ink arrow pointing at a fact, paired with a Scrawl note that adds an honest aside. The shaft draws on with scroll and the head is always present. Under reduced motion both appear finished.

### Stapled Sheet
A full-bleed coloured section. The sheet tilts between -0.4deg and 0.35deg while its content stays level. Each sheet has two drawn staples at its top edge, set about 15% in from each side and rotated a few degrees in opposite directions.

### Bird
The mascot, drawn with a 5px line, a pink body and a yellow beak (the proud beak is pink). Expression marks state: **happy** by default, including the logo and footer, **worried** at a caveat or an attempted action, **proud** on a job well done, **sad** at a limitation. Fill switches to yellow on a teal sheet. Decorative to assistive technology, since the words beside it always carry the meaning.

### Highlighter Bar
A data bar cut from the swipe shape in pink, 28px tall, with its value in marker beside it. Length is strictly proportional to the value across the set, with a 12px floor so the smallest bar remains visible.

### Hand-Drawn Lists
Limits use a marker X and open questions use a marker question mark, in place of bullets. Numbered lists use marker numerals. Doodle icons at 52px sit beside a marker term and a Karla description, with no frame around them.

### Comparison Table
A framed white table with dashed row rules, marker column heads and a solid yellow column for Roost. Competitors share the same type and weight as Roost.

## Do's and Don'ts

### Do:
- **Do** put every page on Photocopy Paper with the toner tile, and bring colour in through full-bleed sheets.
- **Do** keep all text in Marker Ink, and limit Faded Ink to fine print on paper or white.
- **Do** make the finished state the default for every drawn animation, so reduced motion loses nothing.
- **Do** put the `#wob` filter on a separate layer from any text.
- **Do** choose the bird's mood from what the nearby words mean.
- **Do** use one swipe per heading, on the phrase that carries the point.
- **Do** keep controls at least 44px tall, and primary controls and fields at 52px.
- **Do** self-host every font and asset, since the site makes zero third-party requests.

### Don't:
- **Don't** add a dark hero, a dark section or a dark code block.
- **Don't** line up framed icon cards. Doodle icons sit beside text, unframed.
- **Don't** add a fourth hue, or set text in white, pink, yellow or teal.
- **Don't** give cards, buttons or fields a shadow, and don't let the button's second pass fill in as a solid offset block.
- **Don't** tilt the content of a sheet along with the sheet.
- **Don't** set long explanatory text in Permanent Marker.
- **Don't** show a worried, proud or sad bird beside words that don't mean it.
