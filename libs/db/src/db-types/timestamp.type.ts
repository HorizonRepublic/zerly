import { EntityProperty, Platform, Type } from '@mikro-orm/core';

export class TimestampType extends Type<number | null, string> {
  public override convertToJSValue(value: string | number | null | undefined): number | null {
    if (value === null || value === undefined) return null;

    return Number(value);
  }

  public override convertToDatabaseValue(value: number): string {
    return String(value);
  }

  public override getColumnType(prop: EntityProperty, platform: Platform): string {
    return platform.getBigIntTypeDeclarationSQL({});
  }
}
