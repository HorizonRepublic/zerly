import { Injectable } from '@nestjs/common';

/**
 * Demo user record used by the gateway-demo endpoints.
 * @remarks
 * This is an example-app fixture type — it lives alongside the consuming
 * controller rather than in a shared library because the shape is entirely
 * local to this demo. Real production DTOs would be defined alongside the
 * domain module that owns them.
 */
export interface IDemoUser {
  readonly id: string;
  readonly name: string;
}

/**
 * In-memory fixture service backing the gateway-demo controller.
 * @remarks
 * Uses a simple `Map<string, IDemoUser>` pre-seeded with two users. Deletes
 * and creations mutate the map in-process — there is no persistence, no
 * concurrency safety, and no production value beyond demonstrating that the
 * `@GatewayRoute`-decorated handlers integrate cleanly with conventional
 * NestJS service injection.
 */
@Injectable()
export class GatewayDemoService {
  private readonly users = new Map<string, IDemoUser>([
    ['1', { id: '1', name: 'Alice' }],
    ['2', { id: '2', name: 'Bob' }],
  ]);

  /**
   * Look up a user by its string ID.
   * @param id - Opaque user identifier.
   * @returns The matching record, or `null` when no user exists for the ID.
   */
  public findById(id: string): IDemoUser | null {
    return this.users.get(id) ?? null;
  }

  /**
   * Create a new user with a monotonically-increasing numeric ID.
   * @param name - Display name for the new user.
   * @returns The newly created record.
   */
  public create(name: string): IDemoUser {
    const id = String(this.users.size + 1);
    const user: IDemoUser = { id, name };

    this.users.set(id, user);

    return user;
  }

  /**
   * Remove the user identified by `id`. No-op when the ID is unknown so the
   * caller can treat deletes as idempotent.
   * @param id - Opaque user identifier.
   */
  public delete(id: string): void {
    this.users.delete(id);
  }
}
