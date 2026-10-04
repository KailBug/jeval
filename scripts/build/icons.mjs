import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { Resvg } from '@resvg/resvg-js'

const directory = new URL('../../apps/desktop/resources/', import.meta.url)
const svg = readFileSync(new URL('jeval.svg', directory), 'utf8')
const render = (size) => new Resvg(svg, { fitTo: { mode: 'width', value: size } }).render().asPng()

writeFileSync(new URL('jeval.png', directory), render(512))

// Windows ICO directory followed by one PNG payload per supported display size.
const sizes = [16, 20, 24, 32, 40, 48, 64, 128, 256]
const images = sizes.map(render)
const header = Buffer.alloc(6 + 16 * sizes.length)
header.writeUInt16LE(1, 2)
header.writeUInt16LE(sizes.length, 4)
let offset = header.length
for (let index = 0; index < sizes.length; index++) {
  const entry = 6 + index * 16
  header[entry] = sizes[index] === 256 ? 0 : sizes[index]
  header[entry + 1] = header[entry]
  header.writeUInt16LE(1, entry + 4)
  header.writeUInt16LE(32, entry + 6)
  header.writeUInt32LE(images[index].length, entry + 8)
  header.writeUInt32LE(offset, entry + 12)
  offset += images[index].length
}
writeFileSync(new URL('jeval.ico', directory), Buffer.concat([header, ...images]))
console.log(
  `Generated PNG and ${sizes.length}-size ICO from ${fileURLToPath(new URL('jeval.svg', directory))}`
)
