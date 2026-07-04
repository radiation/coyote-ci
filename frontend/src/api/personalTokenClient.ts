import type {
  APIToken,
  APITokenListResponse,
  CreateAPITokenRequest,
  CreatedAPIToken,
} from "../types/identity";
import { deleteNoContent, fetchJSON, postJSON } from "./request";

type DataEnvelope<T> = {
  data: T;
};

export async function listAPITokens(): Promise<APIToken[]> {
  const envelope =
    await fetchJSON<DataEnvelope<APITokenListResponse>>("/me/tokens");
  return envelope.data.tokens;
}

export async function createAPIToken(
  input: CreateAPITokenRequest,
): Promise<CreatedAPIToken> {
  const envelope = await postJSON<
    DataEnvelope<CreatedAPIToken>,
    CreateAPITokenRequest
  >("/me/tokens", input);
  return envelope.data;
}

export async function revokeAPIToken(id: string): Promise<void> {
  await deleteNoContent(`/me/tokens/${encodeURIComponent(id)}`);
}
