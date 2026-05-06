import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createUser, deleteUser, listUsers, updateUser } from "../api";
import type { GlobalRole, User } from "../types/identity";
import { formatTime } from "../utils/time";

export function UsersPage() {
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [globalRole, setGlobalRole] = useState<GlobalRole>("user");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const {
    data: users,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["users"],
    queryFn: listUsers,
  });

  const createMutation = useMutation({
    mutationFn: (input: Parameters<typeof createUser>[0]) => createUser(input),
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      setEmail("");
      setDisplayName("");
      setGlobalRole("user");
    },
    onError: (mutationError) => setErrorMessage(String(mutationError)),
  });

  const updateMutation = useMutation({
    mutationFn: ({ user, input }: { user: User; input: Partial<User> }) =>
      updateUser(user.id, {
        email: input.email,
        display_name: input.display_name,
        global_role: input.global_role,
      }),
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (mutationError) => setErrorMessage(String(mutationError)),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteUser(id),
    onMutate: () => setErrorMessage(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (mutationError) => setErrorMessage(String(mutationError)),
  });

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedEmail = email.trim();
    if (!trimmedEmail) {
      setErrorMessage("Email is required.");
      return;
    }
    createMutation.mutate({
      email: trimmedEmail,
      display_name: displayName.trim() || undefined,
      global_role: globalRole,
    });
  };

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Users</h2>
          <p className="subtle-text">
            Internal identities for trusted-header auth and project membership
            boundaries.
          </p>
        </div>
      </div>

      <section className="settings-panel" style={{ marginTop: 14 }}>
        <h3>Create User</h3>
        <form className="job-form" onSubmit={onSubmit}>
          <label htmlFor="user-email">Email</label>
          <input
            id="user-email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            disabled={createMutation.isPending}
            placeholder="engineer@example.com"
          />

          <label htmlFor="user-display-name">Display Name</label>
          <input
            id="user-display-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            disabled={createMutation.isPending}
            placeholder="Optional"
          />

          <label htmlFor="user-global-role">Global Role</label>
          <select
            id="user-global-role"
            value={globalRole}
            onChange={(event) =>
              setGlobalRole(event.target.value as GlobalRole)
            }
            disabled={createMutation.isPending}
          >
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>

          <div className="job-form-actions">
            <button type="submit" disabled={createMutation.isPending}>
              {createMutation.isPending ? "Creating…" : "Create User"}
            </button>
          </div>
        </form>
      </section>

      {errorMessage && <p className="error-text">{errorMessage}</p>}
      {isLoading && <p>Loading users…</p>}
      {error && (
        <p className="error-text">Failed to load users: {String(error)}</p>
      )}
      {users && users.length === 0 && (
        <p className="subtle-text">No users have been created yet.</p>
      )}

      {users && users.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Email</th>
              <th>Display Name</th>
              <th>Global Role</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <UserRow
                key={user.id}
                user={user}
                onSave={(input) => updateMutation.mutate({ user, input })}
                onDelete={() => deleteMutation.mutate(user.id)}
                disabled={updateMutation.isPending || deleteMutation.isPending}
              />
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}

function UserRow({
  user,
  onSave,
  onDelete,
  disabled,
}: {
  user: User;
  onSave: (input: Partial<User>) => void;
  onDelete: () => void;
  disabled: boolean;
}) {
  const [email, setEmail] = useState(user.email);
  const [displayName, setDisplayName] = useState(user.display_name ?? "");
  const [globalRole, setGlobalRole] = useState<GlobalRole>(user.global_role);

  return (
    <tr>
      <td>
        <input
          aria-label={`Email for ${user.email}`}
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          disabled={disabled}
        />
      </td>
      <td>
        <input
          aria-label={`Display name for ${user.email}`}
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
          disabled={disabled}
        />
      </td>
      <td>
        <select
          aria-label={`Global role for ${user.email}`}
          value={globalRole}
          onChange={(event) => setGlobalRole(event.target.value as GlobalRole)}
          disabled={disabled}
        >
          <option value="user">user</option>
          <option value="admin">admin</option>
        </select>
      </td>
      <td>{user.updated_at ? formatTime(user.updated_at) : "—"}</td>
      <td>
        <div className="table-actions">
          <button
            type="button"
            className="table-action-button"
            onClick={() =>
              onSave({
                email: email.trim(),
                display_name: displayName.trim() || null,
                global_role: globalRole,
              })
            }
            disabled={disabled}
          >
            Save
          </button>
          <button
            type="button"
            className="table-action-button"
            onClick={onDelete}
            disabled={disabled}
          >
            Delete
          </button>
        </div>
      </td>
    </tr>
  );
}
