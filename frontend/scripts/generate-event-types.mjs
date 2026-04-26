import { mkdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { compileFromFile } from 'json-schema-to-typescript'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const frontendDir = path.resolve(scriptDir, '..')

const jobs = [
  {
    schemaPath: path.resolve(frontendDir, '../agent_go/schemas/unified-events-complete.schema.json'),
    outputPath: path.resolve(frontendDir, 'src/generated/events.ts'),
  },
  {
    schemaPath: path.resolve(frontendDir, '../agent_go/schemas/polling-event.schema.json'),
    outputPath: path.resolve(frontendDir, 'src/generated/events-bridge.ts'),
  },
  {
    schemaPath: path.resolve(frontendDir, '../agent_go/schemas/report-plan.schema.json'),
    outputPath: path.resolve(frontendDir, 'src/generated/report-plan.ts'),
  },
]

for (const job of jobs) {
  const output = await compileFromFile(job.schemaPath)
  await mkdir(path.dirname(job.outputPath), { recursive: true })
  await writeFile(job.outputPath, output)
  console.log(`Generated ${path.relative(frontendDir, job.outputPath)}`)
}
