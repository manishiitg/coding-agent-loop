import brightdata from './marks/brightdata.png'
import calcom from './marks/calcom.png'
import chainstack from './marks/chainstack.png'
import close from './marks/close.png'
import context7 from './marks/context7.png'
import deepwiki from './marks/deepwiki.png'
import fireflies from './marks/fireflies.png'
import klaviyo from './marks/klaviyo.png'
import mabl from './marks/mabl.png'
import mobbin from './marks/mobbin.png'
import nslookup from './marks/nslookup.png'
import octagon from './marks/octagon.png'
import plain from './marks/plain.png'
import typeform from './marks/typeform.png'

/**
 * Bitmap marks, for brands that publish no vector logo at all.
 *
 * `brandMarks` is the place to look first — an inline SVG scales, themes with
 * `currentColor`, and costs no request. These brands ship only a raster icon:
 * their site serves a PNG favicon or app icon and nothing else, and neither
 * thesvg nor simple-icons carries them. Tracing a PNG into a "vector" produces a
 * lumpy approximation of someone's trademark, so the original bitmap is kept
 * instead, and the marks committed here are the vendors' own files.
 *
 * Each was downloaded from the vendor's site, cropped to its content, and capped
 * at 128px (never upscaled past 2x its source, so a small favicon stays crisp
 * rather than becoming a soft, fat file). The tile renders at 40px, so 128px
 * covers a 3x display. Vite inlines the small ones and emits the rest as hashed
 * assets.
 *
 * Some arrived as a dark glyph on transparency, which vanishes against the dark
 * tile. Those were composited onto a white rounded plate — context7, deepwiki
 * and mobbin — so every mark clears the same contrast bar in both themes. A
 * bitmap cannot take `currentColor`, so the plate is the only way to make a
 * one-colour raster work on both grounds.
 *
 * A brand in neither map falls through to ConnectionIcon's tinted monogram.
 * Unstructured is the current example: its site serves only a wordmark.
 *
 * Marks remain their owners' trademarks; they are used only to identify the
 * service they belong to.
 */
export const RASTER_MARKS: Record<string, string> = {
  brightdata,
  calcom,
  chainstack,
  close,
  context7,
  deepwiki,
  fireflies,
  klaviyo,
  mabl,
  mobbin,
  nslookup,
  octagon,
  plain,
  typeform,
}
