// Report media must be streamed instead of materialized as an in-memory Blob.
// Keep this list aligned with reportMediaType in the Go report-media endpoint.
const STREAMABLE_REPORT_MEDIA = /\.(?:mp4|m4v|webm|ogv|mp3|m4a|wav|ogg|oga)$/i

export function isStreamableReportMediaPath(path: string): boolean {
  return STREAMABLE_REPORT_MEDIA.test(path)
}
