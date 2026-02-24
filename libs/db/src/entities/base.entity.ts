import { Entity, EntityMetadata, MetadataStorage, Opt, Property, t, Utils } from '@mikro-orm/core';
import { v7 } from 'uuid';

import { IBaseResource } from '@zerly/core';

import { TimestampType } from '../db-types';

// Описуємо конструктор для коректної типізації this у статичних методах
type EntityConstructor<T> = new (...args: unknown[]) => T;

@Entity({ abstract: true })
export abstract class BaseEntity implements IBaseResource {
  @Property({
    name: 'id',
    type: t.uuid,
    onCreate: () => v7(),
    primary: true,
  })
  public id!: Opt<string>;

  @Property({
    name: 'created_at',
    type: TimestampType,
    onCreate: () => Date.now(),
  })
  public createdAt!: Opt<number>;

  @Property({
    name: 'updated_at',
    type: TimestampType,
    onCreate: () => Date.now(),
    onUpdate: () => Date.now(),
  })
  public updatedAt!: Opt<number>;

  @Property({
    name: 'deleted_at',
    type: TimestampType,
    nullable: true,
  })
  public deletedAt: Opt<number> | null = null;

  public static tableName<T extends BaseEntity>(this: EntityConstructor<T>): string {
    const meta = BaseEntity.getMetadata(this);

    return meta.collection;
  }

  public static columns<T extends BaseEntity>(
    this: EntityConstructor<T>,
  ): Record<Extract<keyof T, string>, string> {
    const meta = BaseEntity.getMetadata(this);
    const out: Record<string, string> = {};
    const properties = meta.properties;
    const keys = Object.keys(properties) as (string & keyof typeof properties)[];

    for (const propName of keys) {
      const prop = properties[propName];

      // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- MikroORM typing lies sometimes regarding strictNullChecks
      if (!prop) continue;

      let fieldName: string = propName;

      if (
        Array.isArray(prop.fieldNames) &&
        prop.fieldNames.length > 0 &&
        typeof prop.fieldNames[0] === 'string'
      ) {
        fieldName = prop.fieldNames[0];
      } else if (
        'fieldName' in prop &&
        typeof (prop as { fieldName: unknown }).fieldName === 'string'
      ) {
        fieldName = (prop as { fieldName: string }).fieldName;
      }

      out[propName] = fieldName;
    }

    return out as unknown as Record<Extract<keyof T, string>, string>;
  }

  protected static getMetadata<T>(target: EntityConstructor<T>): EntityMetadata<T> {
    const all = MetadataStorage.getMetadata();
    const key = Utils.className(target);
    const meta = all[key] as EntityMetadata<T> | undefined;

    if (!meta) {
      throw new Error(
        `MikroORM metadata not found for entity "${key}". Did you import the entity module?`,
      );
    }

    return meta;
  }
}
