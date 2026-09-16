// The bounds the server applies to a password (internal/auth.PasswordProblem),
// so a form can say them and refuse to send what would be refused. The minimum
// is in characters; the maximum is bcrypt's 72 bytes, which is twenty-four
// Korean characters — a passphrase reaches that easily, and the form used to
// say only "12자 이상".

export const MIN_PASSWORD_LENGTH = 12;
export const MAX_PASSWORD_BYTES = 72;

export const PASSWORD_RULE = `${MIN_PASSWORD_LENGTH}자 이상, ${MAX_PASSWORD_BYTES}바이트(한글 ${Math.floor(MAX_PASSWORD_BYTES / 3)}자) 이하.`;

export function passwordWithinBounds(password: string): boolean {
  const characters = Array.from(password).length;
  const bytes = new TextEncoder().encode(password).length;
  return characters >= MIN_PASSWORD_LENGTH && bytes <= MAX_PASSWORD_BYTES;
}
