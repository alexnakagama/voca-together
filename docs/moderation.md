# Moderation: reading reports and acting on them

A runbook for the person who reviews reports. It holds only what the code does not say; the design and its
reasons are in decision 033 (`decisions/033-blocking-and-reporting-backend.md`), and the rules the code keeps are
in `.claude/rules/safety.md`.

Members report other members from the app (`PUT /v1/me/reports/{id}`). A report is stored and nothing else
happens: it hides nothing, blocks nobody and notifies nobody. No route returns a report, so **this process, run
by hand with database access, is the only way a report is ever read or acted on.** There is no dashboard and no
status; both are deferred.

## Who reviews, and how often

These two lines are blank on purpose. They are the author's to fill, and until both are filled the gate of
decisions 031 and 033 holds: no feature that lists, suggests or searches members ships, and the app is not
released to the public.

- **Reviewer:** `TO BE FILLED BY THE AUTHOR`
- **Review interval:** `TO BE FILLED BY THE AUTHOR`

## Reading

Connect with `psql` to the production database (`DATABASE_URL`, a secret). The statements below use two `psql`
variables; set each from what the previous statement returned, then run the statement as written:

```text
\set reported_id '<reported_id of the member being reviewed>'
\set report_id '<id of the report being closed>'
```

Every report, oldest first:

```sql
SELECT r.id, r.created_at, r.updated_at, r.reason, r.details, r.reported_id, r.reporter_id
FROM reports r
ORDER BY r.created_at, r.id;
```

One member's reports beside their profile as it is now:

```sql
SELECT r.id, r.created_at, r.updated_at, r.reason, r.details, r.reporter_id,
       p.public_id, p.display_name, p.bio,
       p.updated_at > r.updated_at AS profile_changed_since,
       a.user_id IS NOT NULL AS has_picture,
       a.updated_at > r.updated_at AS picture_changed_since
FROM reports r
LEFT JOIN profiles p ON p.user_id = r.reported_id
LEFT JOIN avatars a ON a.user_id = r.reported_id
WHERE r.reported_id = :'reported_id'
ORDER BY r.created_at, r.id;
```

How to read a row:

- `reason` is one of `harassment`, `inappropriate_content`, `spam`, `impersonation` and `other`. `details` is the
  reporter's own text and may be empty.
- There is one row per reporter and reported member. A member who reports the same member again replaces the
  reason and the details: `updated_at` is when, and `created_at` is still the first time.
- A NULL `reporter_id` means the reporter's account was deleted since. The report is otherwise unchanged.
- NULL in the profile columns means the member has no profile now (they have no public identifier and nothing of
  theirs is readable by other members). A picture without a profile is not public either.

## Acting

Decide on what the profile shows now and on what the reporter wrote. The outcomes, from least to most:

1. **Nothing to do.** Close the report (below).
2. **Take the picture down.** The member can set another.

   ```sql
   DELETE FROM avatars WHERE user_id = :'reported_id';
   ```

3. **Take the profile down.** The name and the text are deleted and the member's public identifier with them:
   nobody can read anything of theirs. The stored picture and languages stay and are not public without a
   profile; take the picture down too if it is the problem. The member can save a profile again, under a new
   identifier, and blocks of them still hold.

   ```sql
   DELETE FROM profiles WHERE user_id = :'reported_id';
   ```

4. **Remove the account.** This cannot be undone. It deletes the account with its sessions, sign-in identities,
   profile, languages, picture, the blocks it made and received, and every report about it. Reports it made
   about others stay, with no reporter. The address can register again.

   ```sql
   DELETE FROM users WHERE id = :'reported_id';
   ```

These are the manual take-downs of decision 031. None of them tells the member why, or that they were reported.

## Closing

A handled report is deleted, whatever the outcome; there is no status column. Removing an account has already
deleted the reports about it.

```sql
DELETE FROM reports WHERE id = :'report_id';
```

If the reporter reports that member again later, it is a new row, which is the wanted behavior.

## Limitations

- **A report holds no copy of what was reported.** The profile may have been edited between the report and the
  review (`profile_changed_since`, `picture_changed_since`), and what it said before is not stored anywhere. The
  reporter's details are the only record of what they saw.
- **A report can be rewritten by its reporter** until it is closed: only the latest reason and details exist.
- **These statements do not show the picture itself,** only whether there is one. It is stored in `avatars.image`
  as a JPEG. How a reviewer looks at it is not decided here.
- **Nothing limits a member who is taken down from coming back:** a profile can be saved again and an address
  can register again. A new report is the remedy.
- **Nothing is recorded about a review:** who looked, when, or what was decided.

## Privacy

- A report's reason and details, and who reported, are never copied into a ticket, a chat, an email or a log,
  and never shown to anyone but the reviewer.
- **The reported member is never told who reported them,** or that a report exists.
- The same holds for what these queries return about the reported member: read it where it is, and take
  nothing out of the database that the task does not need.
