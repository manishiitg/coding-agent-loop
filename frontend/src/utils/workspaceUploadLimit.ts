export const MAX_WORKSPACE_UPLOAD_BYTES = 10 * 1024 * 1024

export function workspaceUploadSizeError(file: Pick<File, 'size'>): string | null {
  return file.size > MAX_WORKSPACE_UPLOAD_BYTES ? 'File exceeds the 10 MB upload limit' : null
}
