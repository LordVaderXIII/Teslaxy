/**
 * Commit a clip-detail response only when it belongs to the latest selection.
 * @param {number | null} requestId
 * @param {number | null} latestId
 * @returns {boolean}
 */
export function shouldCommitDetail(requestId, latestId) {
  if (requestId == null || latestId == null) return false;
  return requestId === latestId;
}
