// RFC 9110 §15.3.5: a 204 is complete at the end of its headers.
// Classify only the exact confirmed deletion with independent durable proof.
// Preserve the original requestfailed record; all other failures stay fatal.
export function confirmedNoContentDeletion(row, proof) {
  return Boolean(proof && proof.client_redirect === true && proof.server_status === 204 &&
    proof.public_status === 410 && proof.database_tombstone === true &&
    proof.database_version === proof.initial_version + 1 && proof.audit_count === 1 &&
    Number.isSafeInteger(proof.initial_version) && proof.initial_version > 0 &&
    row.url === proof.request_url && row.method === 'DELETE' && row.resource_type === 'fetch' &&
    row.navigation === false && row.response_status === 204 && row.failure?.errorText === 'net::ERR_ABORTED');
}
