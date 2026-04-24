echo "post slug:"
read slug
mkdir -p posts/"$slug"/images/raw
echo "---\ntitle: ''\nslug: $slug\ndate: YEAR-MO-DY\nstatus: published\nexcerpt: ''\ncover: IMAGENAME\nrepo: ''\n---" > posts/"$slug"/"$slug".md
echo "post template \""$slug"\" created"