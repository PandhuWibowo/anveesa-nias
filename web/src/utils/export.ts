export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.style.display = 'none'
  document.body.appendChild(a)
  a.click()
  setTimeout(() => {
    URL.revokeObjectURL(url)
    a.remove()
  }, 1000)
}

export function sanitizeFileName(value: string, fallback = 'export') {
  const cleaned = String(value || '')
    .trim()
    .replace(/[^a-z0-9_-]+/gi, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80)
  return cleaned || fallback
}

export function downloadText(text: string, filename: string, type = 'text/plain;charset=utf-8') {
  downloadBlob(new Blob([text], { type }), filename)
}

export function downloadCSV(columns: string[], rows: unknown[][], name = 'export') {
  const escape = (v: unknown): string => {
    if (v === null || v === undefined) return ''
    const s = String(v)
    if (s.includes(',') || s.includes('"') || s.includes('\n')) {
      return '"' + s.replace(/"/g, '""') + '"'
    }
    return s
  }
  const lines = [columns.map(escape).join(',')]
  for (const row of rows) {
    lines.push((row as unknown[]).map(escape).join(','))
  }
  const blob = new Blob(['\ufeff' + lines.join('\n')], { type: 'text/csv;charset=utf-8;' })
  downloadBlob(blob, `${sanitizeFileName(name)}.csv`)
}

export function downloadJSON(columns: string[], rows: unknown[][], name = 'export') {
  const data = rows.map((row) => {
    const obj: Record<string, unknown> = {}
    ;(row as unknown[]).forEach((v, i) => { obj[columns[i]] = v })
    return obj
  })
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  downloadBlob(blob, `${sanitizeFileName(name)}.json`)
}

function escapeHTML(value: unknown) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function escapeXML(value: unknown) {
  return escapeHTML(value).replace(/'/g, '&apos;')
}

function excelWorksheetName(value: string) {
  const cleaned = String(value || 'Sheet')
    .replace(/[\[\]:*?/\\]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 31)
  return cleaned || 'Sheet'
}

export function downloadExcel(columns: string[], rows: unknown[][], name = 'export', title = name) {
  downloadXLSX([{ name: title, rows: [columns, ...rows], headerRows: [1] }], name)
}

export const XLSX_STYLES = {
  normal: 0,
  header: 1,
  title: 2,
  meta: 3,
  muted: 4,
  error: 5,
  badgeBlue: 6,
  badgeTeal: 7,
  badgeAmber: 8,
  badgePurple: 9,
  metricBlue: 10,
  metricTeal: 11,
  metricAmber: 12,
  metricRed: 13,
  label: 14,
} as const

export interface XLSXCell {
  value: unknown
  styleId?: number
}

export interface XLSXSheet {
  name: string
  rows: Array<Array<unknown | XLSXCell>>
  headerRows?: number[]
  titleRows?: number[]
  mutedRows?: number[]
  errorRows?: number[]
  freezeRow?: number
  autoFilterRow?: number
  columnWidths?: number[]
}

function textBytes(value: string) {
  return new TextEncoder().encode(value)
}

const crcTable = (() => {
  const table = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1)
    table[n] = c >>> 0
  }
  return table
})()

function crc32(bytes: Uint8Array) {
  let crc = 0xffffffff
  for (const byte of bytes) crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8)
  return (crc ^ 0xffffffff) >>> 0
}

function writeU16(out: number[], value: number) {
  out.push(value & 0xff, (value >>> 8) & 0xff)
}

function writeU32(out: number[], value: number) {
  out.push(value & 0xff, (value >>> 8) & 0xff, (value >>> 16) & 0xff, (value >>> 24) & 0xff)
}

function zipFiles(files: Array<{ name: string; data: string }>) {
  const out: number[] = []
  const central: number[] = []
  for (const file of files) {
    const name = textBytes(file.name)
    const data = textBytes(file.data)
    const crc = crc32(data)
    const offset = out.length
    writeU32(out, 0x04034b50)
    writeU16(out, 20)
    writeU16(out, 0)
    writeU16(out, 0)
    writeU16(out, 0)
    writeU16(out, 0)
    writeU32(out, crc)
    writeU32(out, data.length)
    writeU32(out, data.length)
    writeU16(out, name.length)
    writeU16(out, 0)
    out.push(...name, ...data)

    writeU32(central, 0x02014b50)
    writeU16(central, 20)
    writeU16(central, 20)
    writeU16(central, 0)
    writeU16(central, 0)
    writeU16(central, 0)
    writeU16(central, 0)
    writeU32(central, crc)
    writeU32(central, data.length)
    writeU32(central, data.length)
    writeU16(central, name.length)
    writeU16(central, 0)
    writeU16(central, 0)
    writeU16(central, 0)
    writeU16(central, 0)
    writeU32(central, 0)
    writeU32(central, offset)
    central.push(...name)
  }
  const centralOffset = out.length
  out.push(...central)
  writeU32(out, 0x06054b50)
  writeU16(out, 0)
  writeU16(out, 0)
  writeU16(out, files.length)
  writeU16(out, files.length)
  writeU32(out, central.length)
  writeU32(out, centralOffset)
  writeU16(out, 0)
  return new Uint8Array(out)
}

function xlsxSheetName(value: string, used: Set<string>) {
  const base = excelWorksheetName(value).slice(0, 28) || 'Sheet'
  let name = base
  let i = 2
  while (used.has(name.toLowerCase())) {
    const suffix = ` ${i++}`
    name = `${base.slice(0, 31 - suffix.length)}${suffix}`
  }
  used.add(name.toLowerCase())
  return name
}

function styledCell(value: unknown): XLSXCell | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  if (!Object.prototype.hasOwnProperty.call(value, 'value')) return null
  const keys = Object.keys(value as Record<string, unknown>)
  if (!keys.every((key) => key === 'value' || key === 'styleId')) return null
  return value as XLSXCell
}

function cellRawValue(value: unknown) {
  return styledCell(value)?.value ?? value
}

function xlsxCell(value: unknown, styleId = 0) {
  const rich = styledCell(value)
  const rawValue = rich ? rich.value : value
  const cellStyleId = rich?.styleId ?? styleId
  const style = cellStyleId ? ` s="${cellStyleId}"` : ''
  if (rawValue === null || rawValue === undefined) return `<c${style} t="inlineStr"><is><t></t></is></c>`
  if (typeof rawValue === 'number' && Number.isFinite(rawValue)) return `<c${style}><v>${rawValue}</v></c>`
  if (typeof rawValue === 'boolean') return `<c${style} t="b"><v>${rawValue ? 1 : 0}</v></c>`
  const text = String(rawValue)
  const numeric = text.trim() !== '' && !Number.isNaN(Number(text)) && !/^0\d+/.test(text.trim())
  if (numeric) return `<c${style}><v>${Number(text)}</v></c>`
  return `<c${style} t="inlineStr"><is><t xml:space="preserve">${escapeXML(text)}</t></is></c>`
}

function xlsxWorksheet(sheet: XLSXSheet) {
  const headerRows = new Set(sheet.headerRows ?? [])
  const titleRows = new Set(sheet.titleRows ?? [])
  const mutedRows = new Set(sheet.mutedRows ?? [])
  const errorRows = new Set(sheet.errorRows ?? [])
  const columnCount = Math.max(1, ...sheet.rows.map((row) => row.length))
  const inferredWidths = inferColumnWidths(sheet.rows, columnCount)
  const columnWidths = Array.from({ length: columnCount }, (_, index) => sheet.columnWidths?.[index] ?? inferredWidths[index])
  const cols = columnWidths
    .map((width, index) => `<col min="${index + 1}" max="${index + 1}" width="${width}" customWidth="1"/>`)
    .join('')
  const sheetViews = sheet.freezeRow
    ? `<sheetViews><sheetView workbookViewId="0" showGridLines="0"><pane ySplit="${sheet.freezeRow}" topLeftCell="A${sheet.freezeRow + 1}" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`
    : '<sheetViews><sheetView workbookViewId="0" showGridLines="0"/></sheetViews>'
  const rows = sheet.rows.map((row, index) => {
    const rowNumber = index + 1
    const styleId = titleRows.has(rowNumber)
      ? 2
      : headerRows.has(rowNumber)
        ? 1
        : errorRows.has(rowNumber)
          ? 5
          : mutedRows.has(rowNumber)
            ? 4
            : 0
    const height = titleRows.has(rowNumber) ? ' ht="30" customHeight="1"' : ''
    return `<row r="${rowNumber}"${height}>${row.map((value) => xlsxCell(value, styleId)).join('')}</row>`
  }).join('')
  const autoFilter = sheet.autoFilterRow
    ? `<autoFilter ref="A${sheet.autoFilterRow}:${columnName(columnCount)}${Math.max(sheet.rows.length, sheet.autoFilterRow)}"/>`
    : ''
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
${sheetViews}
<cols>${cols}</cols>
<sheetData>${rows}</sheetData>
${autoFilter}
</worksheet>`
}

function columnName(index: number) {
  let name = ''
  let n = index
  while (n > 0) {
    const rem = (n - 1) % 26
    name = String.fromCharCode(65 + rem) + name
    n = Math.floor((n - 1) / 26)
  }
  return name
}

function inferColumnWidths(rows: Array<Array<unknown | XLSXCell>>, count: number) {
  return Array.from({ length: count }, (_, col) => {
    const max = Math.max(
      10,
      ...rows.slice(0, 80).map((row) => String(cellRawValue(row[col]) ?? '').replace(/\s+/g, ' ').length),
    )
    return Math.min(48, Math.max(12, max + 2))
  })
}

export function downloadXLSX(sheets: XLSXSheet[], name = 'export') {
  const used = new Set<string>()
  const normalized = sheets.map((sheet) => ({ ...sheet, name: xlsxSheetName(sheet.name, used) }))
  const workbookSheets = normalized.map((sheet, index) =>
    `<sheet name="${escapeXML(sheet.name)}" sheetId="${index + 1}" r:id="rId${index + 1}"/>`,
  ).join('')
  const rels = normalized.map((_, index) =>
    `<Relationship Id="rId${index + 1}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet${index + 1}.xml"/>`,
  ).join('')
  const overrides = normalized.map((_, index) =>
    `<Override PartName="/xl/worksheets/sheet${index + 1}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`,
  ).join('')
  const files = [
    {
      name: '[Content_Types].xml',
      data: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
${overrides}
</Types>`,
    },
    {
      name: '_rels/.rels',
      data: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`,
    },
    {
      name: 'xl/workbook.xml',
      data: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets>${workbookSheets}</sheets>
</workbook>`,
    },
    {
      name: 'xl/_rels/workbook.xml.rels',
      data: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
${rels}
<Relationship Id="rId${normalized.length + 1}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`,
    },
    {
      name: 'xl/styles.xml',
      data: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="10">
  <font><sz val="11"/><color rgb="FF111111"/><name val="Aptos"/></font>
  <font><b/><sz val="10"/><color rgb="FFFFFFFF"/><name val="Aptos"/></font>
  <font><b/><sz val="18"/><color rgb="FFFFFFFF"/><name val="Aptos Display"/></font>
  <font><b/><sz val="10"/><color rgb="FF1E2A3A"/><name val="Aptos"/></font>
  <font><sz val="10"/><color rgb="FF8E9BAD"/><name val="Aptos"/></font>
  <font><b/><sz val="10"/><color rgb="FFA32D2D"/><name val="Aptos"/></font>
  <font><b/><sz val="10"/><color rgb="FF185FA5"/><name val="Aptos"/></font>
  <font><b/><sz val="10"/><color rgb="FF0F6E56"/><name val="Aptos"/></font>
  <font><b/><sz val="10"/><color rgb="FF854F0B"/><name val="Aptos"/></font>
  <font><b/><sz val="10"/><color rgb="FF7F77DD"/><name val="Aptos"/></font>
</fonts>
<fills count="9">
  <fill><patternFill patternType="none"/></fill>
  <fill><patternFill patternType="gray125"/></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FF1E2A3A"/><bgColor indexed="64"/></patternFill></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FFF7F8FA"/><bgColor indexed="64"/></patternFill></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FFFCEBEB"/><bgColor indexed="64"/></patternFill></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FFE6F1FB"/><bgColor indexed="64"/></patternFill></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FFE1F5EE"/><bgColor indexed="64"/></patternFill></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FFFAEEDA"/><bgColor indexed="64"/></patternFill></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FFEEEDFE"/><bgColor indexed="64"/></patternFill></fill>
</fills>
<borders count="2">
  <border/>
  <border><left style="thin"><color rgb="FFD0D7E2"/></left><right style="thin"><color rgb="FFD0D7E2"/></right><top style="thin"><color rgb="FFD0D7E2"/></top><bottom style="thin"><color rgb="FFD0D7E2"/></bottom></border>
</borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="15">
  <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1"><alignment vertical="top" wrapText="1"/></xf>
  <xf numFmtId="0" fontId="1" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="center"/></xf>
  <xf numFmtId="0" fontId="2" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment horizontal="center" vertical="center"/></xf>
  <xf numFmtId="0" fontId="3" fillId="3" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="center"/></xf>
  <xf numFmtId="0" fontId="4" fillId="3" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="top" wrapText="1"/></xf>
  <xf numFmtId="0" fontId="5" fillId="4" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="top" wrapText="1"/></xf>
  <xf numFmtId="0" fontId="6" fillId="5" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment horizontal="center" vertical="center"/></xf>
  <xf numFmtId="0" fontId="7" fillId="6" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment horizontal="center" vertical="center"/></xf>
  <xf numFmtId="0" fontId="8" fillId="7" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment horizontal="center" vertical="center"/></xf>
  <xf numFmtId="0" fontId="9" fillId="8" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment horizontal="center" vertical="center"/></xf>
  <xf numFmtId="0" fontId="6" fillId="5" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="center"/></xf>
  <xf numFmtId="0" fontId="7" fillId="6" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="center"/></xf>
  <xf numFmtId="0" fontId="8" fillId="7" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="center"/></xf>
  <xf numFmtId="0" fontId="5" fillId="4" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="center"/></xf>
  <xf numFmtId="0" fontId="3" fillId="0" borderId="1" xfId="0" applyFont="1" applyBorder="1"><alignment vertical="center"/></xf>
</cellXfs>
</styleSheet>`,
    },
    ...normalized.map((sheet, index) => ({ name: `xl/worksheets/sheet${index + 1}.xml`, data: xlsxWorksheet(sheet) })),
  ]
  const blob = new Blob([zipFiles(files)], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
  downloadBlob(blob, `${sanitizeFileName(name)}.xlsx`)
}

function collectDocumentStyles() {
  const styles: string[] = []
  for (const sheet of Array.from(document.styleSheets)) {
    try {
      styles.push(Array.from(sheet.cssRules).map((rule) => rule.cssText).join('\n'))
    } catch {
      // Ignore cross-origin stylesheets.
    }
  }
  return styles.join('\n')
}

function cloneForExport(element: HTMLElement, width: number) {
  const clone = element.cloneNode(true) as HTMLElement
  clone.querySelectorAll('[data-export-ignore="true"]').forEach((node) => node.remove())
  clone.querySelectorAll('input, textarea, select, button').forEach((node) => {
    if ((node as HTMLElement).closest('[data-export-keep="true"]')) return
    node.remove()
  })
  clone.style.width = `${width}px`
  clone.style.maxWidth = `${width}px`
  clone.style.margin = '0'
  clone.style.overflow = 'visible'
  return clone
}

export async function downloadElementPNG(element: HTMLElement, name = 'export') {
  await document.fonts?.ready
  const rect = element.getBoundingClientRect()
  const width = Math.max(1, Math.ceil(rect.width))
  const height = Math.max(1, Math.ceil(element.scrollHeight || rect.height))
  const clone = cloneForExport(element, width)
  const styles = collectDocumentStyles()
  const html = `<div xmlns="http://www.w3.org/1999/xhtml">
<style>
${styles}
* { box-sizing: border-box; }
body { margin: 0; }
[data-export-ignore="true"] { display: none !important; }
.adb-full, .adb-dashboard-canvas { height: auto !important; min-height: auto !important; overflow: visible !important; }
.adb-grid { align-items: stretch !important; }
</style>
${clone.outerHTML}
</div>`
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">
<foreignObject width="100%" height="100%">${html}</foreignObject>
</svg>`
  const url = URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml;charset=utf-8' }))
  try {
    try {
      const image = new Image()
      image.decoding = 'async'
      image.src = url
      await image.decode()
      const canvas = document.createElement('canvas')
      const scale = Math.min(2, window.devicePixelRatio || 1)
      canvas.width = Math.ceil(width * scale)
      canvas.height = Math.ceil(height * scale)
      const ctx = canvas.getContext('2d')
      if (!ctx) throw new Error('canvas unavailable')
      ctx.fillStyle = getComputedStyle(document.body).getPropertyValue('--bg-body') || '#ffffff'
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      ctx.scale(scale, scale)
      ctx.drawImage(image, 0, 0, width, height)
      const blob = await new Promise<Blob>((resolve, reject) => {
        canvas.toBlob((value) => value ? resolve(value) : reject(new Error('image export failed')), 'image/png')
      })
      downloadBlob(blob, `${sanitizeFileName(name)}.png`)
    } catch {
      throw new Error('PNG export failed because the browser blocked canvas export')
    }
  } finally {
    URL.revokeObjectURL(url)
  }
}

export function printElementPDF(element: HTMLElement, title = 'export') {
  const rect = element.getBoundingClientRect()
  const width = Math.max(1, Math.ceil(rect.width))
  const clone = cloneForExport(element, width)
  const styles = collectDocumentStyles()
  const win = window.open('', '_blank', 'width=1200,height=800')
  if (!win) throw new Error('popup blocked')
  win.document.write(`<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<title>${escapeHTML(title)}</title>
<style>
${styles}
@page { size: auto; margin: 12mm; }
body { margin: 0; background: var(--bg-body, #fff); color: var(--text-primary, #111); }
[data-export-ignore="true"] { display: none !important; }
.page-shell, .adb-full-shell, .adb-full { min-height: auto !important; height: auto !important; overflow: visible !important; }
.adb-dashboard-canvas { height: auto !important; overflow: visible !important; }
.adb-grid { align-items: stretch !important; }
</style>
</head>
<body>${clone.outerHTML}</body>
</html>`)
  win.document.close()
  win.focus()
  setTimeout(() => {
    win.print()
  }, 250)
}
