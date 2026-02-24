import typia from 'typia';

export interface IBaseResource {
  id: string & typia.tags.Format<'uuid'>;

  createdAt: number & typia.tags.Type<'uint64'>;

  updatedAt: number & typia.tags.Type<'uint64'>;

  deletedAt: (number & typia.tags.Type<'uint64'>) | null;
}
